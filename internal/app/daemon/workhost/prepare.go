package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (h *Host) Prepare(ctx context.Context, need workengine.WorkerNeed) error {
	key := need.ContextID
	if key == "" {
		workstream := "main"
		if need.TaskID != "" {
			workstream = "task:" + need.TaskID
		}
		key = idFor("context", need.ScopeID, need.RoleID, workstream)
	}
	unlock, err := h.preparations.acquire(ctx, key)
	if err != nil {
		return err
	}
	defer unlock()
	select {
	case h.launchSlots <- struct{}{}:
		defer func() { <-h.launchSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	var scope work.WorkScope
	var role work.Role
	var wc work.WorkContext
	var binding work.RoleBotBinding
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		if scope, err = tx.GetScope(need.ScopeID); err != nil {
			return err
		}
		if role, err = tx.GetRole(need.RoleID); err != nil {
			return err
		}
		if binding, err = tx.GetRoleBotBinding(role.ID); err != nil {
			return err
		}
		contextID := need.ContextID
		workstream := "main"
		if need.TaskID != "" {
			workstream = "task:" + need.TaskID
		}
		if contextID == "" {
			contextID = idFor("context", scope.ID, role.ID, workstream)
		}
		wc, err = tx.GetContext(contextID)
		if errors.Is(err, workstore.ErrNotFound) {
			wc = work.WorkContext{ID: contextID, ScopeID: scope.ID, RoleID: role.ID, TaskID: need.TaskID, Workstream: workstream, Revision: 1, CreatedAt: h.config.Now(), UpdatedAt: h.config.Now()}
			return tx.PutContext(wc, 0)
		}
		if err != nil {
			return err
		}
		if wc.ScopeID != scope.ID || wc.RoleID != role.ID || wc.TaskID != need.TaskID {
			return fmt.Errorf("worker preparation context mismatch")
		}
		return nil
	})
	if err != nil {
		return err
	}
	workerID := idFor("worker", wc.ID)
	var previous *work.WorkerSession
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker(workerID)
		if err == nil {
			previous = &worker
			return nil
		}
		if errors.Is(err, workstore.ErrNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	settings, err := h.config.Settings.ResolveLaunch(ctx, scope, role, wc)
	if err != nil {
		return err
	}
	if err = h.freezeUpgradeBinary(ctx, &settings); err != nil {
		return err
	}
	if err = h.freezeReviewLocation(ctx, wc, &settings); err != nil {
		return err
	}
	if settings.Location.HostID != h.config.HostID || !filepath.IsAbs(settings.Location.Cwd) {
		return fmt.Errorf("launch location must belong to this explicit host")
	}
	if err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.AssertRuntimeResourceAvailable("cwd:" + settings.Location.HostID + ":" + settings.Location.Cwd)
	}); err != nil {
		return err
	}
	info, err := os.Stat(settings.Location.Cwd)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("execution location is not a directory")
	}
	if settings.Backend == work.BackendCodex && (settings.CodexResume == nil || settings.CodexResume.ModelProviderID == "") {
		return fmt.Errorf("Codex requires a complete resolved provider policy, including defaults")
	}
	bundle, err := h.service.BuildInstructions(ctx, wc.ID)
	if err != nil {
		return err
	}
	bundle = composeLaunchInstructions(settings, reviewInstructionBundle(wc, bundle))
	if previous != nil {
		rebuild, err := h.prepareExisting(ctx, previous, role, binding, settings, bundle)
		if err != nil || !rebuild {
			return err
		}
	}
	if err = h.ensurePoolCapacity(ctx, wc.ID, wc.Purpose); err != nil {
		return err
	}
	incarnation, err := opaqueID("carrier")
	if err != nil {
		return err
	}
	instanceID, err := opaqueID("inst-work")
	if err != nil {
		return err
	}
	worker := work.WorkerSession{ID: workerID, ContextID: wc.ID, ScopeID: scope.ID, RoleID: role.ID, Backend: settings.Backend, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: instanceID, CarrierIncarnation: incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationID: settings.Location.ID, LocationGeneration: settings.Location.Generation, DesiredSettingsRevision: role.SettingsRevision, AppliedSettingsRevision: role.SettingsRevision, Revision: 1, UpdatedAt: h.config.Now()}
	if previous != nil {
		worker.Generation = previous.Generation + 1
		worker.ControllerGeneration = previous.ControllerGeneration + 1
		worker.InstanceGeneration = previous.InstanceGeneration + 1
		worker.ProcessGeneration = previous.ProcessGeneration + 1
		worker.Revision = previous.Revision + 1
		worker.State = work.WorkerRecovering
	}
	owner := agentproto.ExecutionBinding{Version: agentproto.WorkerProtocolVersion, ScopeID: scope.ID, ContextID: wc.ID, RoleID: role.ID, WorkerSessionID: worker.ID, SessionGeneration: worker.Generation, ControllerGeneration: worker.ControllerGeneration, BotGeneration: binding.IdentityGeneration}
	snapshot := agentproto.RuntimeLaunchSnapshot{NativeBinaryPath: settings.NativeBinaryPath, NativeBinaryVersion: settings.NativeBinaryVersion, ReadOnly: settings.ReadOnly, HostID: h.config.HostID, NativeMemoryPolicyVersion: agentproto.NativeMemoryPolicyVersion, Version: agentproto.WorkerProtocolVersion, Owner: owner, CWD: settings.Location.Cwd, InstructionBundle: bundle, ToolEndpoint: h.config.ToolEndpoint, SessionCredentialRef: "env:" + config.WorkSessionTokenEnv, JournalPath: filepath.Join(h.config.Paths.StateDir, "workers", worker.ID, "runtime.db"), CarrierIncarnation: incarnation, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: settings.Location.Generation}
	record := LaunchRecord{PreferencesRevision: settings.PreferencesRevision, WorkerID: worker.ID, ScopeID: scope.ID, Generation: worker.Generation, RoleDescriptionVersion: role.DescriptionVersion, SettingsRevision: role.SettingsRevision, ProfileID: settings.ProfileID, Backend: settings.Backend, Snapshot: snapshot, Overrides: settings.Overrides, CodexResume: settings.CodexResume}
	if err = snapshot.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	content := work.NewContent(idFor("launch_body", worker.ID, fmt.Sprint(worker.Generation)), scope.ID, "application/json", data, h.config.Now())
	op := work.Operation{ID: launchID(worker), ScopeID: scope.ID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: launchID(worker), RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: h.config.Now(), UpdatedAt: h.config.Now()}
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		freshRole, e := tx.GetRole(role.ID)
		if e != nil {
			return e
		}
		freshBinding, e := tx.GetRoleBotBinding(role.ID)
		if e != nil {
			return e
		}
		if freshRole.Revision != role.Revision || freshBinding != binding {
			return workstore.ErrConflict
		}
		if existing, e := tx.GetLocation(settings.Location.ID); errors.Is(e, workstore.ErrNotFound) {
			if e = tx.PutLocation(settings.Location, 0); e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else if existing.Generation != settings.Location.Generation || existing.Cwd != settings.Location.Cwd || existing.HostID != settings.Location.HostID {
			return workstore.ErrConflict
		}
		expected := int64(0)
		if previous != nil {
			expected = previous.Revision
		}
		count, e := tx.CountNativePoolCarriers(h.config.HostID)
		if e != nil {
			return e
		}
		if count >= h.config.MaxLiveCarriers {
			return ErrNativePoolCapacity
		}
		if wc.Purpose != work.PurposeControl {
			count, e = tx.CountBusinessNativePoolCarriers(h.config.HostID)
			if e != nil {
				return e
			}
			if count >= h.config.MaxLiveCarriers-h.config.ControlReservedCarriers {
				return ErrNativePoolCapacity
			}
		}
		if e = h.assertUpgradeLaunch(ctx, tx, worker.Backend); e != nil {
			return e
		}
		if e = tx.PutWorker(worker, expected); e != nil {
			return e
		}
		if e = tx.PutContent(content); e != nil {
			return e
		}
		_, _, e = tx.PutOperation(op, 0)
		return e
	})
	if err != nil {
		return err
	}
	carrier := workstore.CarrierRegistration{Incarnation: incarnation, OriginInstanceID: instanceID, Owner: owner, Backend: worker.Backend, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration}
	if err = h.config.Registry.RegisterCarrier(ctx, carrier); err != nil {
		return err
	}
	token, err := h.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: role.ID, BotGeneration: binding.IdentityGeneration, ScopeID: scope.ID, ContextID: wc.ID, WorkerID: worker.ID, SessionGeneration: worker.Generation, ControllerGeneration: worker.ControllerGeneration}, 30*24*time.Hour)
	if err != nil {
		return err
	}
	return h.startPrepared(ctx, worker, op, record, settings, token)
}
