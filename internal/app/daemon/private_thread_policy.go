package daemon

import (
	"context"
	"os"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// refreshPrivateThreadPolicy takes snapshots outside the Processor lock; these
// control presentation only. Resume independently rechecks and reserves.
func (g *applicationIngress) refreshPrivateThreadPolicy(ctx context.Context, h *privateNativeHost, p *privatechat.Processor, owner privatechat.Owner, generation int64) error {
	binding, err := g.privatePublicationBindingFor(ctx, owner, generation)
	if err != nil {
		return err
	}
	target := executionowner.Owner{Application: executionowner.Private, ID: binding.Owner.ID, Epoch: binding.Owner.Epoch}
	policy := orchestrator.PrivateThreadPolicy{InstanceNamespaces: map[string]string{}, CatalogNamespaces: map[agentproto.Backend]string{}, Denied: map[orchestrator.PrivateHistoryKey]bool{}, ManagedDenied: map[orchestrator.PrivateHistoryKey]bool{}, InternalDenied: map[orchestrator.PrivateHistoryKey]bool{}, Ambiguous: map[orchestrator.PrivateHistoryKey]bool{}, Transferred: map[orchestrator.PrivateHistoryKey]bool{}}
	for _, state := range []string{"prepared", "sending", "running", "draining", "sealed", "retired"} {
		records, e := h.store.byState(ctx, state)
		if e != nil {
			return e
		}
		for _, r := range records {
			policy.InstanceNamespaces[r.Snapshot.Owner.InstanceID] = r.Snapshot.Owner.HistoryNamespace
		}
	}
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	// The persisted Codex catalog always opens HOME/.codex, even when the
	// daemon environment contains CODEX_HOME. Identify the actual catalog root.
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	if ns, e := nativeHistoryNamespace(agentproto.BackendCodex, []string{"HOME=" + home}, cwd, h.hostID); e == nil {
		policy.CatalogNamespaces[agentproto.BackendCodex] = ns
	}
	if ns, e := nativeHistoryNamespace(agentproto.BackendClaude, os.Environ(), cwd, h.hostID); e == nil {
		policy.CatalogNamespaces[agentproto.BackendClaude] = ns
	}
	if ns, e := nativeHistoryNamespace(agentproto.BackendOpenCode, os.Environ(), cwd, h.hostID); e == nil {
		policy.CatalogNamespaces[agentproto.BackendOpenCode] = ns
	}
	denied, ambiguous, err := teamHistoryPolicySnapshot(ctx, h.teamHistory)
	if err != nil {
		return err
	}
	for _, k := range denied {
		policy.Denied[k] = true
	}
	for _, k := range ambiguous {
		policy.Ambiguous[k] = true
	}
	internalSessions, err := h.journal.InternalNativeSessions(ctx)
	if err != nil {
		return err
	}
	for _, v := range internalSessions {
		policy.InternalDenied[orchestrator.PrivateHistoryKey{Backend: agentproto.Backend(v.Backend), Namespace: v.Namespace, Session: v.SessionID}] = true
	}
	claims, err := h.registry.HistoryClaims(ctx)
	if err != nil {
		return err
	}
	for _, c := range claims {
		k := orchestrator.PrivateHistoryKey{Backend: agentproto.Backend(c.Backend), Namespace: c.Namespace, Session: c.Session}
		known, allowed, e := h.registry.SessionAccess(ctx, c.Backend, c.Namespace, c.Session, target)
		if e != nil {
			return e
		}
		if known && !allowed {
			policy.ManagedDenied[k] = true
		}
		if allowed {
			transferred, e := h.registry.SessionTransferred(ctx, c.Backend, c.Namespace, c.Session, target)
			if e != nil {
				return e
			}
			if transferred {
				policy.Transferred[k] = true
			}
		}
	}
	return p.SetThreadPolicy(ctx, owner, generation, policy)
}
