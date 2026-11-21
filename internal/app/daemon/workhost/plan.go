package workhost

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (h *Host) Plan(ctx context.Context, candidate workengine.Candidate) (workengine.DispatchPlan, error) {
	var record LaunchRecord
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		if gate, e := tx.GetRuntimeGate(candidate.Worker.ID); e == nil && gate.Reason == "snapshot_rebuild_required" && gate.RecoveryReason == "" {
			return fmt.Errorf("%w: a retired carrier requires fresh preparation", workengine.ErrPreparationRequired)
		}
		record, err = h.readLaunch(tx, candidate.Worker)
		return err
	})
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	if err := h.checkDesiredLocation(ctx, candidate.Worker); err != nil {
		return workengine.DispatchPlan{}, err
	}
	if record.Snapshot.ReadOnly != (candidate.Context.Purpose == work.PurposeReview) {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: native read-only policy changed", workengine.ErrPreparationRequired)
	}
	if record.Snapshot.HostID != h.config.HostID {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: explicit carrier host identity requires fresh preparation", workengine.ErrPreparationRequired)
	}
	if record.Snapshot.NativeMemoryPolicyVersion != agentproto.NativeMemoryPolicyVersion {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: native memory policy requires a fresh carrier and native history", workengine.ErrPreparationRequired)
	}
	if candidate.Role.SettingsRevision != record.SettingsRevision || candidate.Role.DescriptionVersion != record.RoleDescriptionVersion {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: role settings changed; worker needs a verified safe rebuild", workengine.ErrPreparationRequired)
	}
	if record.Snapshot.Owner.BotGeneration != candidate.BotBinding.IdentityGeneration {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: bot identity changed; old worker cannot acquire new work", workengine.ErrPreparationRequired)
	}
	bundle, err := h.service.BuildInstructions(ctx, candidate.Context.ID)
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	settings, err := h.config.Settings.ResolveLaunch(ctx, candidate.Scope, candidate.Role, candidate.Context)
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	if err = h.freezeUpgradeBinary(ctx, &settings); err != nil {
		return workengine.DispatchPlan{}, err
	}
	if err = h.freezeReviewLocation(ctx, candidate.Context, &settings); err != nil {
		return workengine.DispatchPlan{}, err
	}
	bundle = composeLaunchInstructions(settings, reviewInstructionBundle(candidate.Context, bundle))
	if !sameRuntimePolicy(record, settings) || settings.ProfileID != record.ProfileID || settings.Backend != record.Backend || settings.Location.ID != candidate.Worker.LocationID || settings.Location.Generation != candidate.Worker.LocationGeneration || settings.Location.Cwd != record.Snapshot.CWD {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: resolved launch configuration changed", workengine.ErrPreparationRequired)
	}
	if bundle.Revision != record.Snapshot.InstructionBundle.Revision {
		return workengine.DispatchPlan{}, fmt.Errorf("%w: scoped memory or instructions changed", workengine.ErrPreparationRequired)
	}
	commandID, err := opaqueID("command")
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	attemptID, err := opaqueID("attempt")
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	runID, err := opaqueID("run")
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	attempt := work.ExecutionAttempt{ID: attemptID, ScopeID: candidate.Scope.ID, ContextID: candidate.Context.ID, RunRef: work.RunRef{WorkerID: candidate.Worker.ID, Generation: candidate.Worker.Generation, RunID: runID}, State: work.AttemptDispatchPending, CommandID: commandID, Revision: 1}
	if candidate.Task != nil {
		attempt.TaskID = candidate.Task.ID
		attempt.FrozenRequirementsRevision = candidate.Task.RequirementsRevision
		attempt.FrozenAssignmentGeneration = candidate.Task.AssignmentGeneration
	}
	for _, input := range candidate.Inputs {
		attempt.InputIDs = append(attempt.InputIDs, input.ID)
	}
	inputs, err := h.service.BuildPlannedWorkPrompt(ctx, attempt, filepath.Join(h.config.Paths.StateDir, "team-attachments"))
	if err != nil {
		return workengine.DispatchPlan{}, err
	}
	if candidate.Context.Purpose == work.PurposeReview {
		if candidate.Context.Review == nil {
			return workengine.DispatchPlan{}, fmt.Errorf("missing frozen review specification")
		}
		diffInput, e := buildReviewDiffInput(ctx, record.Snapshot.CWD, candidate.Context.Review.Target)
		if e != nil {
			return workengine.DispatchPlan{}, e
		}
		inputs = append(inputs, diffInput)
	}
	binding := record.Snapshot.Owner
	binding.AttemptID, binding.RunRef = attemptID, runID
	mode := agentproto.PromptExecutionModeStartNew
	if candidate.Worker.NativeSessionID != "" {
		mode = agentproto.PromptExecutionModeResumeExisting
	}
	command := agentproto.Command{CommandID: commandID, IssuedAt: h.config.Now(), Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: record.Snapshot.InstructionBundle.Revision, Target: agentproto.Target{ExecutionMode: mode, ThreadID: candidate.Worker.NativeSessionID, CWD: record.Snapshot.CWD, SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection}, Prompt: agentproto.Prompt{Inputs: inputs}, Overrides: record.Overrides, CodexResume: record.CodexResume}
	refs := []work.SourceRef{{Kind: "instruction_bundle", ID: candidate.Worker.ID, Version: record.Snapshot.InstructionBundle.Revision, EvidenceRef: idFor("launch_body", candidate.Worker.ID, fmt.Sprint(candidate.Worker.Generation))}}
	return workengine.DispatchPlan{WorkerRevision: candidate.Worker.Revision, InstructionRevision: record.Snapshot.InstructionBundle.Revision, Command: command, FrozenRefs: refs}, nil
}
