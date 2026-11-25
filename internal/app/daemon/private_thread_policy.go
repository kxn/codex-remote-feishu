package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// refreshPrivateThreadPolicy takes snapshots outside the Processor lock; these
// control presentation only. Resume independently rechecks and reserves.
func (g *applicationIngress) refreshPrivateThreadPolicy(ctx context.Context, h *privateNativeHost, p *privatechat.Processor, owner privatechat.Owner, generation int64, aliasScanSequence ...int64) error {
	binding, err := g.privatePublicationBindingFor(ctx, owner, generation)
	if err != nil {
		return err
	}
	target := executionowner.Owner{Application: executionowner.Private, ID: binding.Owner.ID, Epoch: binding.Owner.Epoch}
	policy := orchestrator.PrivateThreadPolicy{InstanceNamespaces: map[string]string{}, InstanceProfiles: map[string]string{}, CatalogNamespaces: map[agentproto.Backend]string{}, Denied: map[orchestrator.PrivateHistoryKey]bool{}, ManagedDenied: map[orchestrator.PrivateHistoryKey]bool{}, InternalDenied: map[orchestrator.PrivateHistoryKey]bool{}, Ambiguous: map[orchestrator.PrivateHistoryKey]bool{}, Transferred: map[orchestrator.PrivateHistoryKey]bool{}, LegacyAliases: map[orchestrator.PrivateHistoryKey]bool{}}
	namespaces := map[string]bool{}
	for _, state := range []string{"prepared", "sending", "running", "draining", "sealed", "retired"} {
		records, e := h.store.byState(ctx, state)
		if e != nil {
			return e
		}
		for _, r := range records {
			policy.InstanceNamespaces[r.Snapshot.Owner.InstanceID] = r.Snapshot.Owner.HistoryNamespace
			policy.InstanceProfiles[r.Snapshot.Owner.InstanceID] = r.CodexProfileID
			if r.Surface == owner && r.SurfaceGeneration == generation && r.Snapshot.Owner.Backend == agentproto.BackendCodex && r.Snapshot.Owner.HistoryNamespace != "" {
				namespaces[r.Snapshot.Owner.HistoryNamespace] = true
			}
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
		namespaces[ns] = true
	}
	if ns, e := nativeHistoryNamespace(agentproto.BackendClaude, os.Environ(), cwd, h.hostID); e == nil {
		policy.CatalogNamespaces[agentproto.BackendClaude] = ns
	}
	if ns, e := nativeHistoryNamespace(agentproto.BackendOpenCode, os.Environ(), cwd, h.hostID); e == nil {
		policy.CatalogNamespaces[agentproto.BackendOpenCode] = ns
	}
	denied, ambiguous, err := teamHistoryPolicySnapshot(ctx, h.teamHistory)
	if err != nil {
		return fmt.Errorf("读取团队历史失败: %w", err)
	}
	for _, k := range denied {
		policy.Denied[k] = true
	}
	for _, k := range ambiguous {
		policy.Ambiguous[k] = true
	}
	if g.host.applications != nil && g.host.applications.migrations != nil {
		if len(aliasScanSequence) > 0 && aliasScanSequence[0] > 0 {
			c := g.host.applications.migrations
			key := legacyHistoryCacheKey{Owner: owner, Generation: generation}
			value, _ := c.historyScans.LoadOrStore(key, &legacyHistoryScanState{})
			scan := value.(*legacyHistoryScanState)
			scan.mu.Lock()
			profile, profileErr := g.effectiveLegacyHistoryProfile(ctx, owner, generation)
			if profileErr == nil && (aliasScanSequence[0] > scan.Sequence || profile != scan.Profile) && (scan.FailedAt.IsZero() || time.Since(scan.FailedAt) >= 2*time.Second) {
				if g.refreshLegacyHistoryAliases(ctx, owner, generation, namespaces, ambiguous) {
					scan.Sequence, scan.Profile, scan.FailedAt = aliasScanSequence[0], profile, time.Time{}
				} else {
					scan.FailedAt = time.Now()
				}
			}
			scan.mu.Unlock()
		}
		policy.LegacyAliases, policy.LegacyReadOnly = g.cachedLegacyHistoryAliases(ctx, owner, generation)
		if profile, e := g.effectiveLegacyHistoryProfile(ctx, owner, generation); e == nil {
			policy.LegacyProfile = profile
		}
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

// preparePrivateHistory snapshots history only for an explicit presentation or
// selection. Identity errors still fail admission; source-read errors become
// an owner-scoped, fail-closed visible history error.
func (g *applicationIngress) preparePrivateHistory(ctx context.Context, h *privateNativeHost, p *privatechat.Processor, owner privatechat.Owner, generation, sequence int64) error {
	if _, err := g.privatePublicationBindingFor(ctx, owner, generation); err != nil {
		return err
	}
	if err := g.refreshPrivateThreadPolicy(ctx, h, p, owner, generation, sequence); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return p.SetThreadPolicy(ctx, owner, generation, orchestrator.PrivateThreadPolicy{Unavailable: err.Error()})
	}
	return nil
}

func (h *privateNativeHost) updateThreadCarrier(ctx context.Context, p *privatechat.Processor, rec privateCarrierRecord) error {
	return p.UpdateThreadCarrier(ctx, rec.Surface, rec.SurfaceGeneration, rec.Snapshot.Owner.InstanceID, rec.Snapshot.Owner.HistoryNamespace, rec.CodexProfileID)
}

func shouldPreparePrivateHistory(ctx context.Context, p *privatechat.Processor, owner privatechat.Owner, generation int64, raw []byte) (bool, error) {
	if !privateHistoryPolicyNeedsRefresh(raw) {
		return false, nil
	}
	var action control.Action
	if err := json.Unmarshal(raw, &action); err != nil {
		return false, err
	}
	draft, err := p.IsHistoryDraftNavigation(ctx, owner, generation, action)
	return !draft, err
}
