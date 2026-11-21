package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type teamLegacyFencer struct{ app *App }

func (h teamLegacyFencer) Fence(_ context.Context, snapshot workmigration.Snapshot) (workmigration.FenceReceipt, error) {
	a := h.app
	if a.teamRuntime == nil {
		return workmigration.FenceReceipt{}, fmt.Errorf("team runtime missing")
	}
	a.mu.Lock()
	a.teamRuntime.legacyFenced.Store(true)
	a.sendAgentCommand = func(string, agentproto.Command) error { return fmt.Errorf("legacy execution authority retired") }
	a.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 0, fmt.Errorf("legacy launch authority retired")
	}
	a.mu.Unlock()
	receipt := workmigration.FenceReceipt{MigrationID: snapshot.ID, SourceFingerprint: snapshot.SourceFingerprint, LegacyWritersClosed: true, QueuesCaptured: true, IngressClosed: true, CronDisabled: true, InflightReconciled: true}
	receipt.Evidence, _ = json.Marshal(map[string]any{"phase": "before_gateway_start", "legacy_tick_and_action_fenced": true, "queue_source": "available in-memory records and original durable files", "limitation": "legacy queues absent from durable files cannot be reconstructed after prior process exit"})
	for _, record := range snapshot.Records {
		if record.Native != nil {
			evidence, _ := json.Marshal(map[string]any{"record_key": record.Key, "reason": "legacy protocol has no complete native/continuation/background quiescence proof"})
			receipt.Native = append(receipt.Native, workmigration.NativeFence{RecordKey: record.Key, Classification: "unknown", Evidence: evidence})
		}
	}
	return receipt, nil
}

func (a *App) activateWorkRuntime(ctx context.Context, snapshot workmigration.Snapshot) error {
	r := a.teamRuntime
	coordinator, err := workmigration.New(r.store, teamLegacyFencer{a}, workmigration.Options{StateDir: a.headlessRuntime.Paths.StateDir})
	if err != nil {
		return err
	}
	r.migration = coordinator
	// This snapshot is frozen once; a restarted daemon must not replace original
	// queues with its newly empty legacy in-memory service.
	path := filepath.Join(a.headlessRuntime.Paths.StateDir, "migrations", teamMigrationID, "ready.json")
	if data, e := os.ReadFile(path); e == nil {
		if err = json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	} else {
		if err = a.captureTeamLegacyMemory(&snapshot); err != nil {
			return err
		}
		data, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		if err = writeTeamImmutable(path, data); err != nil {
			return err
		}
	}
	status, err := coordinator.Prepare(ctx, snapshot)
	if err != nil {
		return err
	}
	// The durable phase may be committed already, but every new host process
	// must install the in-process writer fence before gateways can start.
	if _, err = (teamLegacyFencer{a}).Fence(ctx, snapshot); err != nil {
		return err
	}
	if status, err = coordinator.Fence(ctx, snapshot.ID); err != nil {
		return err
	}
	for status.Phase == "quiescing" {
		if status, err = coordinator.Import(ctx, snapshot.ID, 256); err != nil {
			return err
		}
	}
	if status.Phase == "imported" {
		if _, err = coordinator.Commit(ctx, snapshot.ID); err != nil {
			return err
		}
	}
	if _, err = coordinator.Finalize(ctx, snapshot.ID); err != nil {
		return err
	}
	r.active.Store(true)
	return nil
}

func (a *App) captureTeamLegacyMemory(snapshot *workmigration.Snapshot) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Legacy connection/PID metadata does not prove which host owns a process.
	// Preserve its reported path without attributing a remote wrapper locally.
	// An active native instance can outlive its old surface. Preserve and fence
	// that evidence separately; current UI attachment is not execution ownership.
	for _, instance := range a.service.Instances() {
		if instance == nil || instance.ActiveTurnID == "" {
			continue
		}
		raw, err := json.Marshal(instance)
		if err != nil {
			return err
		}
		cwd := instance.WorkspaceRoot
		if thread := instance.Threads[instance.ActiveThreadID]; thread != nil && thread.CWD != "" {
			cwd = thread.CWD
		}
		snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{
			Key: "active-instance:" + instance.InstanceID, UnitKey: "instance:" + instance.InstanceID,
			Kind: "native", Raw: raw,
			Native: &workmigration.LegacyNative{Backend: work.Backend(instance.Backend), Namespace: string(instance.Backend),
				SessionID: instance.ActiveThreadID, InstanceID: instance.InstanceID, CWD: cwd, Facts: raw},
		})
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		raw, err := json.Marshal(surface)
		if err != nil {
			return err
		}
		chatType := ""
		if ref, ok := feishuidentity.ParseSurfaceRef(surface.SurfaceSessionID); ok {
			if ref.IsUser() {
				chatType = "p2p"
			} else {
				chatType = "group"
			}
		}
		snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{Key: "live-surface:" + surface.SurfaceSessionID, UnitKey: surface.SurfaceSessionID, Kind: "surface", ChatType: chatType, Raw: raw})
		ids := make([]string, 0, len(surface.QueueItems))
		for id := range surface.QueueItems {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		inflight := false
		for _, id := range ids {
			item := surface.QueueItems[id]
			if item == nil {
				continue
			}
			data, err := json.Marshal(item)
			if err != nil {
				return err
			}
			queue := &workmigration.LegacyQueue{OriginalID: id, SourceRef: item.SourceMessageID}
			// Original state has no per-input receipt timestamp. Preserve the whole
			// queue but do not manufacture one that would silently auto-replay it.
			for _, input := range item.Inputs {
				if input.Type == "text" {
					queue.Text += input.Text + "\n"
				}
			}
			snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{Key: "queue:" + surface.SurfaceSessionID + ":" + id, UnitKey: surface.SurfaceSessionID, Kind: "queue", ChatType: chatType, Raw: data, Queue: queue})
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning, state.QueueItemSteering, state.QueueItemShellCommanding, state.QueueItemShellUnknown:
				inflight = true
			}
		}
		if inflight {
			cwd := surface.ClaimedWorkspaceKey
			if surface.PreparedThreadCWD != "" {
				cwd = surface.PreparedThreadCWD
			}
			snapshot.Records = append(snapshot.Records, workmigration.LegacyRecord{Key: "inflight:" + surface.SurfaceSessionID, UnitKey: surface.SurfaceSessionID, Kind: "native", ChatType: chatType, Raw: raw, Native: &workmigration.LegacyNative{Backend: work.Backend(surface.Backend), Namespace: string(surface.Backend), SessionID: surface.SelectedThreadID, InstanceID: surface.AttachedInstanceID, CWD: cwd, Facts: raw}})
		}
	}
	return nil
}

func (a *App) observeLegacyWork(ctx context.Context, instance, kind string, value any) bool {
	r := a.teamRuntime
	if r == nil || !r.legacyFenced.Load() {
		return false
	}
	raw, err := json.Marshal(value)
	if err == nil && r.migration != nil {
		if hello, ok := value.(agentproto.Hello); kind == "hello" && ok {
			err = r.migration.ObserveLegacyHello(ctx, hello, workmigration.HostAttribution{})
		} else {
			err = r.migration.ObserveLegacy(ctx, instance, kind, raw)
		}
	}
	if err != nil {
		log.Printf("team legacy observation failed: instance=%s kind=%s error=%v", instance, kind, err)
	}
	return true
}

func (a *App) authorizeTeamControl(ctx context.Context, actor work.Actor, scopeID, workerID string) error {
	if actor.Kind != work.ActorPrincipal {
		return workstore.ErrScopeDenied
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, err := tx.GetScope(scopeID)
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker(workerID)
		if err != nil {
			return err
		}
		if worker.ScopeID != scope.ID {
			return workstore.ErrScopeDenied
		}
		if scope.Kind == work.ScopePersonal {
			if scope.PrincipalID == actor.ID {
				return nil
			}
			return workstore.ErrScopeDenied
		}
		return tx.RequireProjectPrincipalAccess(scope.ID, actor.ID, time.Now().UTC())
	})
}
