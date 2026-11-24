package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type legacyHistoryCacheKey struct {
	Owner      privatechat.Owner
	Generation int64
}
type legacyHistoryScanState struct {
	mu       sync.Mutex
	Sequence int64
	Profile  string
	FailedAt time.Time
}

type legacyHistoryCache struct {
	Scope, Profile string
	Keys           map[orchestrator.PrivateHistoryKey]bool
	Ready          map[orchestrator.PrivateHistoryKey]bool
}

func historyAliasNeedsRefresh(raw []byte) bool {
	var a control.Action
	if json.Unmarshal(raw, &a) != nil {
		return false
	}
	switch a.Kind {
	case control.ActionListInstances, control.ActionShowThreads, control.ActionShowAllThreads, control.ActionShowScopedThreads, control.ActionShowWorkspaceThreads, control.ActionShowAllThreadWorkspaces, control.ActionShowRecentThreadWorkspaces, control.ActionUseThread:
		return true
	case control.ActionTextMessage:
		text := strings.TrimSpace(a.Text)
		return text == "/list" || text == "/use" || strings.HasPrefix(text, "/use ")
	}
	return false
}

// refreshLegacyHistoryAliases performs expensive root enumeration only for an
// explicit history/list selection. Cache is presentation only: native launch
// and command submission revalidate full evidence and source quiescence.
func (g *applicationIngress) refreshLegacyHistoryAliases(ctx context.Context, owner privatechat.Owner, generation int64, namespaces map[string]bool, ambiguous []orchestrator.PrivateHistoryKey) bool {
	c := g.host.applications.migrations
	if c == nil {
		return false
	}
	key := legacyHistoryCacheKey{owner, generation}
	source, _, err := g.legacyHistorySource(ctx, owner, generation)
	if err != nil {
		c.historyAliases.Delete(key)
		return false
	}
	entry := legacyHistoryCache{Scope: source.Source.ScopeID, Profile: state.NormalizeCodexProfileID(source.Settings.ProfileID), Keys: map[orchestrator.PrivateHistoryKey]bool{}, Ready: map[orchestrator.PrivateHistoryKey]bool{}}
	for _, candidate := range ambiguous {
		if candidate.Backend != agentproto.BackendCodex {
			continue
		}
		for namespace := range namespaces {
			ok, e := g.legacyHistoryAllowed(ctx, owner, generation, candidate.Backend, namespace, candidate.Session, false)
			if e != nil {
				return false
			}
			if ok {
				key := orchestrator.PrivateHistoryKey{Backend: candidate.Backend, Namespace: namespace, Session: candidate.Session}
				entry.Keys[key] = true
				ready, re := g.legacyHistoryAllowed(ctx, owner, generation, candidate.Backend, namespace, candidate.Session, true)
				if re != nil {
					return false
				}
				entry.Ready[key] = ready
			}
		}
	}
	c.historyAliases.Store(key, entry)
	return true
}

// cachedLegacyHistoryAliases repeats the cheap per-carrier conflict inventory;
// a newly recorded foreign/team owner immediately hides an old cached result.
func (g *applicationIngress) cachedLegacyHistoryAliases(ctx context.Context, owner privatechat.Owner, generation int64) (map[orchestrator.PrivateHistoryKey]bool, map[orchestrator.PrivateHistoryKey]bool) {
	result := map[orchestrator.PrivateHistoryKey]bool{}
	readonly := map[orchestrator.PrivateHistoryKey]bool{}
	c := g.host.applications.migrations
	if c == nil {
		return result, readonly
	}
	value, ok := c.historyAliases.Load(legacyHistoryCacheKey{owner, generation})
	if !ok {
		return result, readonly
	}
	entry := value.(legacyHistoryCache)
	profile, err := g.effectiveLegacyHistoryProfile(ctx, owner, generation)
	if err != nil || profile != entry.Profile {
		return result, readonly
	}
	for key := range entry.Keys {
		conflict := false
		err = g.host.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			carriers, e := tx.HistoricalSessionCarriers(key.Session)
			if e != nil {
				return e
			}
			for _, carrier := range carriers {
				if carrier.Backend != work.BackendCodex || carrier.Owner.ScopeID == entry.Scope {
					continue
				}
				op, e := tx.GetOperation(workhost.LaunchOperationID(carrier.Owner.WorkerSessionID, carrier.Owner.SessionGeneration))
				if e != nil {
					conflict = true
					continue
				}
				body, e := tx.GetContent(carrier.Owner.ScopeID, op.RequestRef)
				if e != nil {
					return e
				}
				var launch workhost.LaunchRecord
				if json.Unmarshal(body.Data, &launch) != nil || launch.ScopeID != carrier.Owner.ScopeID || launch.WorkerID != carrier.Owner.WorkerSessionID || launch.Generation != carrier.Owner.SessionGeneration || launch.Snapshot.CarrierIncarnation != carrier.Incarnation || launch.Backend != carrier.Backend || launch.NativeHistoryNamespace == "" || launch.NativeHistoryNamespace == key.Namespace {
					conflict = true
				}
			}
			return nil
		})
		if err == nil && !conflict {
			result[key] = true
			readonly[key] = !entry.Ready[key]
		}
	}
	return result, readonly
}

func effectiveLegacyCodexProfile(record state.BotCapabilitySettingsRecord, checkpointProfile string) string {
	if strings.TrimSpace(record.CodexProfileID) != "" {
		return state.NormalizeCodexProfileID(record.CodexProfileID)
	}
	return state.NormalizeCodexProfileID(checkpointProfile)
}

// The config owner's confirmed record has the same precedence as
// Service.surfaceCodexProfileID. The cache is accepted only at its current
// owner revision, and is populated by refreshPrivateBotSettings outside p.mu.
func (g *applicationIngress) effectiveLegacyHistoryProfile(ctx context.Context, owner privatechat.Owner, generation int64) (string, error) {
	cp, err := g.host.applications.journal.LoadCheckpoint(ctx, owner, generation)
	if err != nil {
		return "", err
	}
	value, ok := g.host.privateSettingsCache.Load(owner.GatewayID)
	if ok {
		entry, valid := value.(privateBotSettingsCacheEntry)
		if valid && entry.Owner == owner && entry.Snapshot.Revision == g.host.privateSettingsRevision.Load() {
			return effectiveLegacyCodexProfile(entry.Snapshot.Record, cp.Core.CodexProfileID), nil
		}
	}
	snapshot, err := g.host.readPrivateBotSettings(owner.GatewayID, owner.AppID)
	if err != nil {
		return "", err
	}
	return effectiveLegacyCodexProfile(snapshot.Record, cp.Core.CodexProfileID), nil
}
