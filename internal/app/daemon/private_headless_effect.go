package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

type privateHeadlessIntent struct {
	Version                                int
	EffectID, InstanceID, Incarnation, CWD string
	Owner                                  privatechat.Owner
	Generation                             int64
}
type privateHeadlessReceipt struct {
	Version              int
	EffectID, InstanceID string
	PID                  int
	Success              bool
	Problem              string
	ProblemCode          string `json:",omitempty"`
}
type privateHeadlessReducer struct{ host *App }

func (r privateHeadlessReducer) ReduceHostReceipt(app *privatechat.App, e privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(e.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateHeadlessReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != e.ID || event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandStartHeadless || receipt.InstanceID != event.DaemonCommand.InstanceID {
		return nil, privatechat.ErrJournalConflict
	}
	if receipt.Success {
		if receipt.PID <= 0 {
			return nil, privatechat.ErrJournalConflict
		}
		return app.Service().HandleHeadlessLaunchStarted(event.SurfaceSessionID, receipt.InstanceID, receipt.PID), nil
	}
	if receipt.Problem == "" {
		return nil, privatechat.ErrJournalConflict
	}
	var cause error = errors.New(receipt.Problem)
	if receipt.ProblemCode == "opencode_upgrade_required" {
		cause = agentproto.ErrorInfo{Code: receipt.ProblemCode, Message: "当前 OpenCode 版本不支持单聊，请升级到 OpenCode 2.1.4 或更新的 2.x 版本。"}
	}
	return app.Service().HandleHeadlessLaunchFailed(event.SurfaceSessionID, receipt.InstanceID, cause), nil
}

// runPrivateHeadlessStart uses the durable host launch state to reconcile crash
// windows. It never retries a sending launch with an unknown process identity.
func runPrivateHeadlessStart(ctx context.Context, host *App, native *privateNativeHost, ingress *applicationIngress, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	effect, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandStartHeadless || cmd.SurfaceSessionID != o.SurfaceID || event.SurfaceSessionID != o.SurfaceID || cmd.InstanceID == "" {
		return nil, privatechat.ErrJournalConflict
	}
	upgradePlan, upgradeRequired, err := j.UpgradeRecoverySelection(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	if effect.State == "confirmed" {
		if upgradeRequired {
			var receipt privateHeadlessReceipt
			if json.Unmarshal(effect.Receipt, &receipt) != nil || !receipt.Success {
				return nil, privatechat.ErrDeferred
			}
			applied, e := j.UpgradeRecoveryEffectApplied(ctx, o, generation, id)
			if e != nil {
				return nil, e
			}
			if !applied {
				if err = p.RestoreUpgradeHeadlessProjection(ctx, o, generation, id); err != nil {
					return nil, err
				}
			}
		}
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateHeadlessReducer{host})
	}
	if effect.State != "prepared" && effect.State != "unknown" && effect.State != "sending" {
		return nil, privatechat.ErrJournalState
	}
	plan, migrationRequired, err := j.MigrationResumeSelection(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	var migration *privatechat.MigrationResumeIntent
	if migrationRequired && upgradeRequired {
		return nil, privatechat.ErrJournalConflict
	}
	var upgrade *privatechat.UpgradeRecoveryPlan
	if upgradeRequired {
		if upgradePlan.OldOwner.Backend != cmd.Backend || upgradePlan.ThreadID != cmd.ThreadID || upgradePlan.ProfileID != cmd.CodexProfileID || upgradePlan.CWD != pathcanon.Native(headlessCommandWorkspaceKey(*cmd)) {
			return nil, privatechat.ErrJournalConflict
		}
		upgrade = &upgradePlan
		if err = p.RestoreUpgradeHeadlessProjection(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	if migrationRequired {
		if native == nil || native.verifyMigrationResume == nil {
			return nil, privatechat.ErrDeferred
		}
		if err = native.verifyMigrationResume(ctx, o, generation, id, plan); err != nil {
			return nil, errors.Join(privatechat.ErrDeferred, err)
		}
		legacyCodex := plan.EvidenceVersion == "legacy-codex-v1" && cmd.Backend == agentproto.BackendCodex && plan.LegacyEvidenceID != "" && plan.LegacyEvidenceHash != "" && plan.ProfileID == cmd.CodexProfileID
		currentClaude := (plan.EvidenceVersion == "" || plan.EvidenceVersion == "launch-v1") && cmd.Backend == agentproto.BackendClaude && plan.ConnectionIdentity != "" && plan.ProfileID == cmd.ClaudeProfileID
		if plan.Backend != string(cmd.Backend) || plan.ThreadID == "" || plan.ThreadID != cmd.ThreadID || plan.Namespace == "" || plan.CWD != pathcanon.Native(headlessCommandWorkspaceKey(*cmd)) || (!legacyCodex && !currentClaude) {
			return nil, privatechat.ErrJournalConflict
		}
		migration = &plan
		if err = p.RestoreMigrationHeadlessProjection(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	if native == nil || ingress == nil {
		return nil, privatechat.ErrDeferred
	}
	if effect.State == "prepared" {
		superseded := false
		if err = p.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
			surface := app.Service().Surface(o.SurfaceID)
			if surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != cmd.InstanceID {
				superseded = true
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if superseded {
			if upgrade != nil {
				return nil, privatechat.ErrDeferred
			}
			if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
				return nil, err
			}
			return confirmPrivateHeadlessFailure(ctx, host, p, j, o, generation, effect, cmd.InstanceID, errors.New("headless launch superseded"))
		}
	}
	binding, err := ingress.privatePublicationBindingFor(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(id + "\x00" + binding.Owner.ID))
	incarnation := "private_" + hex.EncodeToString(sum[:20])
	cwd := pathcanon.Native(headlessCommandWorkspaceKey(*cmd))
	intent := privateHeadlessIntent{Version: 1, EffectID: id, InstanceID: cmd.InstanceID, Incarnation: incarnation, CWD: cwd, Owner: o, Generation: generation}
	intentRaw, _ := json.Marshal(intent)
	stored, e := j.LoadLocalIntent(ctx, o, generation, id)
	if errors.Is(e, sql.ErrNoRows) && effect.State == "prepared" {
		if e = j.PrepareLocalIntent(ctx, o, generation, id, intentRaw); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else if string(stored) != string(intentRaw) {
		return nil, privatechat.ErrJournalConflict
	}
	if effect.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	rec, lookupErr := native.store.lookup(ctx, cmd.InstanceID)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		// Launch always persists its carrier before spawning. Recheck cancellation
		// after a crash between marking this effect sending and preparing that
		// record, so recovery cannot resurrect an already-cancelled launch.
		superseded := false
		if err = p.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
			surface := app.Service().Surface(o.SurfaceID)
			superseded = surface == nil || surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID != cmd.InstanceID
			return nil
		}); err != nil {
			return nil, err
		}
		if superseded {
			if upgrade != nil {
				return nil, privatechat.ErrDeferred
			}
			return confirmPrivateHeadlessFailure(ctx, host, p, j, o, generation, effect, cmd.InstanceID, errors.New("headless launch superseded"))
		}
		nativeOwner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: binding.Owner.ID, Epoch: binding.Owner.Epoch, CarrierIncarnation: incarnation, InstanceID: cmd.InstanceID, HostID: native.hostID, InstanceGeneration: 1, ProcessGeneration: 1, Backend: cmd.Backend}
		if upgrade != nil {
			nativeOwner.InstanceGeneration = upgrade.OldOwner.InstanceGeneration + 1
			nativeOwner.ProcessGeneration = upgrade.OldOwner.ProcessGeneration + 1
			nativeOwner.HistoryNamespace = upgrade.OldOwner.HistoryNamespace
		}
		if migration != nil {
			nativeOwner.HistoryNamespace = migration.Namespace
		}
		rec = privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: nativeOwner, CWD: cwd, JournalPath: filepath.Join(native.stateDir, "private-carriers", incarnation, "runtime.db")}, Surface: o, SurfaceGeneration: generation}
		_, err = host.launchPrivateNative(ctx, native, privateNativeLaunchRequest{Record: rec, OpenCodeProfileID: cmd.OpenCodeProfileID, OpenCodeAdmissionRef: cmd.OpenCodeAdmissionRef, OpenCodeRuntimeAccessMode: cmd.OpenCodeRuntimeAccessMode, CodexProfileID: cmd.CodexProfileID, ClaudeProfileID: cmd.ClaudeProfileID, ClaudeReasoningEffort: cmd.ClaudeReasoningEffort, ResumeSession: cmd.ThreadID, CodexAdmissionRef: cmd.CodexAdmissionRef, MigrationResume: migration, UpgradeRecovery: upgrade, UpgradeRecoveryEffectID: id})
		if err != nil {
			if migration != nil || upgrade != nil {
				return nil, errors.Join(privatechat.ErrDeferred, err)
			}
			if _, e = native.store.lookup(ctx, cmd.InstanceID); errors.Is(e, sql.ErrNoRows) {
				return confirmPrivateHeadlessFailure(ctx, host, p, j, o, generation, effect, cmd.InstanceID, err)
			}
			return nil, privatechat.ErrDeferred
		}
		rec, err = native.store.lookup(ctx, cmd.InstanceID)
		if err != nil {
			return nil, err
		}
	} else if lookupErr != nil {
		return nil, lookupErr
	}
	if rec.Surface != o || rec.SurfaceGeneration != generation || rec.Snapshot.Owner.OwnerID != binding.Owner.ID || rec.Snapshot.Owner.Epoch != binding.Owner.Epoch || rec.Snapshot.Owner.CarrierIncarnation != incarnation || rec.Snapshot.CWD != cwd || !reflect.DeepEqual(rec.MigrationResume, migration) || !reflect.DeepEqual(rec.UpgradeRecovery, upgrade) {
		return nil, privatechat.ErrJournalConflict
	}
	if rec.State == "prepared" {
		if _, err = host.resumePreparedPrivateLaunch(ctx, native, cmd.InstanceID); err != nil {
			return nil, privatechat.ErrDeferred
		}
		rec, err = native.store.lookup(ctx, cmd.InstanceID)
		if err != nil {
			return nil, err
		}
	}
	if rec.State == "sending" {
		return nil, privatechat.ErrDeferred
	}
	if rec.State != "running" || rec.Process.PID <= 0 {
		return nil, privatechat.ErrJournalConflict
	}
	receipt := privateHeadlessReceipt{Version: 1, EffectID: id, InstanceID: cmd.InstanceID, PID: rec.Process.PID, Success: true}
	return confirmPrivateHeadlessReceipt(ctx, host, p, j, o, generation, effect, receipt)
}
func confirmPrivateHeadlessFailure(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, effect privatechat.EffectRecord, instance string, cause error) ([]privatechat.EffectRecord, error) {
	receipt := privateHeadlessReceipt{Version: 1, EffectID: effect.Effect.ID, InstanceID: instance, Problem: cause.Error()}
	var problem agentproto.ErrorInfo
	if errors.As(cause, &problem) && problem.Code == "opencode_upgrade_required" {
		receipt.ProblemCode = problem.Code
	}
	return confirmPrivateHeadlessReceipt(ctx, host, p, j, o, generation, effect, receipt)
}
func confirmPrivateHeadlessReceipt(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, effect privatechat.EffectRecord, receipt privateHeadlessReceipt) ([]privatechat.EffectRecord, error) {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	if err = j.ConfirmLocalEffect(ctx, o, generation, effect.Effect.ID, raw, privateLocalVerifier{owner: o, generation: generation, effect: effect.Effect, receipt: raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, effect.Effect.ID, privateHeadlessReducer{host})
}
