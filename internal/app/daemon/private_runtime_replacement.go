package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// WithRetiredRuntime reuses the persisted registry retirement and the same
// owner-scoped native inspection used by the original stop path.
func (h *privateNativeHost) WithRetiredRuntime(ctx context.Context, o privatechat.Owner, generation int64, old agentproto.NativeOwner, commit func() error) error {
	rec, err := h.store.lookup(ctx, old.InstanceID)
	if err != nil {
		return err
	}
	if rec.State != "retired" || rec.Surface != o || rec.SurfaceGeneration != generation || rec.Snapshot.Owner != old || rec.Inspection.Incarnation != old.CarrierIncarnation {
		return privatechat.ErrJournalConflict
	}
	reg := privateRegistration(old)
	evidence := privateInspectionEvidence(rec.Inspection)
	if err = h.registry.Retire(ctx, reg, evidence); err != nil {
		return err
	}
	if err = h.VerifyStop(ctx, reg, evidence); err != nil {
		return err
	}
	return commit()
}

// prepareRetiredRuntime is called for a persisted owner before its input is
// reduced. A non-retired or unresolved carrier never grants replacement.
func (r *privateWorkRunner) prepareRetiredRuntime(ctx context.Context, o privatechat.Owner, generation int64) (bool, error) {
	cp, err := r.journal.LoadCheckpoint(ctx, o, generation)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	nav := cp.Core.Navigation
	for _, hold := range cp.Core.Holds {
		if hold.Kind == "headless_launch" {
			var pending state.HeadlessLaunchRecord
			if json.Unmarshal(hold.Frozen, &pending) != nil {
				return false, privatechat.ErrJournalConflict
			}
			if pending.Purpose == state.HeadlessLaunchPurposePrivateUpgrade && pending.AutoRestore && pending.SourceInstanceID == cp.Core.AttachedInstanceID {
				return true, nil
			}
		}
	}
	if !retiredCheckpointHasTarget(cp.Core) {
		return false, nil
	}
	rec, err := r.native.store.lookup(ctx, cp.Core.AttachedInstanceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if rec.State != "retired" || rec.Surface != o || rec.SurfaceGeneration != generation || (nav != nil && rec.Snapshot.Owner != nav.Owner) {
		return false, nil
	}
	if _, err = r.ingress.privatePublicationBindingFor(ctx, o, generation); err != nil {
		return true, err
	}
	session := cp.Core.SelectedThreadID
	if session != "" {
		known, allowed, e := r.native.registry.SessionAccess(ctx, string(rec.Snapshot.Owner.Backend), rec.Snapshot.Owner.HistoryNamespace, session, privateRegistration(rec.Snapshot.Owner).Owner)
		if e != nil {
			return true, e
		}
		if !known || !allowed {
			return true, privatechat.ErrJournalConflict
		}
	}
	if err = r.processor.RecordRetiredThread(ctx, o, generation, rec.Snapshot.Owner.Backend, rec.Snapshot.Owner.InstanceID, rec.Snapshot.Owner.HistoryNamespace, session); err != nil {
		return true, err
	}
	contract := orchestrator.PrivateRetiredContract{Profiles: orchestrator.PrivateNativeProfiles{Codex: rec.CodexProfileID, Claude: rec.ClaudeProfileID, OpenCode: rec.OpenCodeProfileID}, CodexAdmissionRef: rec.CodexAdmissionRef, ClaudeReasoningEffort: rec.ClaudeReasoningEffort, OpenCodeAdmissionRef: rec.OpenCodeAdmissionRef, OpenCodeRuntimeAccessMode: rec.OpenCodeRuntimeAccessMode}
	effects, err := r.processor.PrepareRetiredRuntimeReplacement(ctx, o, generation, rec.Snapshot.Owner, contract, rec.Snapshot.CWD, r.native)
	if errors.Is(err, orchestrator.ErrPrivateRuntimeProfileChanged) {
		return false, err
	}
	if err != nil {
		return true, err
	}
	return len(effects) > 0, nil
}

// runtimeReplacementForEffect identifies only the existing pending private
// replacement marker; unrelated automatic thread restores remain unchanged.
func runtimeReplacementForEffect(ctx context.Context, j *privatechat.Journal, o privatechat.Owner, generation int64, cmd *control.DaemonCommand, h *privateNativeHost) (*privateCarrierRecord, error) {
	if cmd == nil || !cmd.AutoRestore {
		return nil, nil
	}
	if h == nil || h.store == nil {
		return nil, privatechat.ErrDeferred
	}
	cp, err := j.LoadCheckpoint(ctx, o, generation)
	if err != nil {
		return nil, err
	}
	var pending state.HeadlessLaunchRecord
	found := false
	for _, hold := range cp.Core.Holds {
		if hold.Kind != "headless_launch" {
			continue
		}
		var candidate state.HeadlessLaunchRecord
		if json.Unmarshal(hold.Frozen, &candidate) != nil {
			return nil, privatechat.ErrJournalConflict
		}
		if candidate.InstanceID == cmd.InstanceID && candidate.Purpose == state.HeadlessLaunchPurposePrivateUpgrade && candidate.AutoRestore {
			if found {
				return nil, privatechat.ErrJournalConflict
			}
			pending = candidate
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	if pending.SourceInstanceID != cp.Core.AttachedInstanceID || (cp.Core.Navigation != nil && pending.SourceInstanceID != cp.Core.Navigation.Owner.InstanceID) || (cp.Core.Navigation == nil && (cp.Core.RouteMode != state.RouteModeNewThreadReady || cp.Core.HistoricalInstance == nil || cp.Core.HistoricalInstance.InstanceID != pending.SourceInstanceID)) || pending.ThreadID != cmd.ThreadID || pending.ThreadCWD != cmd.ThreadCWD {
		return nil, privatechat.ErrJournalConflict
	}
	rec, err := h.store.lookup(ctx, pending.SourceInstanceID)
	if err != nil {
		return nil, err
	}
	if rec.State != "retired" || rec.Surface != o || rec.SurfaceGeneration != generation || (cp.Core.Navigation != nil && rec.Snapshot.Owner != cp.Core.Navigation.Owner) {
		return nil, privatechat.ErrJournalConflict
	}
	return &rec, nil
}

// retiredCheckpointHasTarget selects only a frozen selected session or an
// explicitly prepared new-session workspace, never an inferred directory.
func retiredCheckpointHasTarget(c orchestrator.PrivateCheckpoint) bool {
	if c.AttachedInstanceID == "" {
		return false
	}
	if c.Navigation != nil {
		return true
	}
	if c.HistoricalInstance == nil || c.HistoricalInstance.InstanceID != c.AttachedInstanceID {
		return false
	}
	return (c.RouteMode == state.RouteModeNewThreadReady && c.PreparedThreadCWD != "") || (c.RouteMode == state.RouteModePinned && c.SelectedThreadID != "")
}

type retiredProjectionAuthority struct{ host *privateNativeHost }

func (a retiredProjectionAuthority) WithNativeLease(ctx context.Context, n agentproto.NativeOwner, o privatechat.Owner, generation int64, fn func() error) error {
	return a.host.WithRetiredRuntime(ctx, o, generation, n, fn)
}

func (h *privateNativeHost) projectRetiredCarrierEvents(ctx context.Context, rec privateCarrierRecord) error {
	pending, err := h.journal.HasPendingNativeProjection(ctx, rec.Snapshot.Owner.CarrierIncarnation)
	if err != nil || !pending {
		return err
	}
	return h.processor.ApplyStoppedNativeEvents(ctx, retiredProjectionAuthority{host: h}, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration)
}
