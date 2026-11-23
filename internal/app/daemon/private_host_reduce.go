package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// privateHostReducer is invoked exclusively under Processor's lock. The host
// must never concurrently use its legacy handler or a second service owner.
// Controlled read-only local filesystem observations are permitted; their
// resulting views are frozen in the committed effects. No external writes,
// process launches, gateway or native I/O are permitted here.
type privateHostReducer struct{ host *App }

func (r privateHostReducer) check(app *privatechat.App) error {
	if r.host == nil || app == nil || r.host.service != app.Service() || r.host.projector == nil {
		return fmt.Errorf("private host does not own private service")
	}
	return nil
}

func (r privateHostReducer) Reduce(app *privatechat.App, action control.Action) ([]eventcontract.Event, error) {
	if err := r.check(app); err != nil {
		return nil, err
	}
	if err := app.ValidateAction(action); err != nil {
		return nil, err
	}
	action = r.classify(action)
	if flow := app.Service().PrivateUpgradeFlow(action.SurfaceSessionID); flow != nil && (flow.Phase == "running" || flow.Phase == "cancelling" || flow.Phase == "restarting") {
		switch action.Kind {
		case control.ActionTextMessage, control.ActionImageMessage, control.ActionFileMessage, control.ActionSteerAll, control.ActionNewThread, control.ActionAttachInstance, control.ActionAttachWorkspace, control.ActionUseThread:
			return nil, privatechat.ErrDeferred
		}
	}
	if action.Kind == control.ActionPrivateProject {
		return r.projectAction(action)
	}
	if events, handled, err := r.privateHistoryRecovery(app, action); handled {
		return events, err
	}
	if notice := rejectedInboundNotice(action); notice != nil {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: notice}}, nil
	}
	if privateSettingsWriteNeedsOwnerReceipt(action) {
		switch action.Kind {
		case control.ActionAccessCommand:
			return app.Action(action)
		case control.ActionModeCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand:
			return r.reduceSettingsTransitionIntent(app, action)
		}
		surface := app.Service().Surface(action.SurfaceSessionID)
		if surface == nil || surface.Backend != agentproto.BackendCodex {
			return nil, privatechat.ErrDeferred
		}
		return r.reduceSettingsIntent(app, action)
	}
	// File staging is a separate durable effect handled by the private runner.
	if action.Kind == control.ActionFileMessage || (action.Kind == control.ActionTextMessage && len(action.Files) != 0) {
		surface := app.Service().Surface(action.SurfaceSessionID)
		if surface != nil && app.Service().Instance(surface.AttachedInstanceID) != nil {
			instance := app.Service().Instance(surface.AttachedInstanceID)
			if strings.TrimSpace(instance.WorkspaceRoot) == "" {
				return nil, privatechat.ErrDeferred
			}
			raw, err := json.Marshal(action)
			if err != nil {
				return nil, err
			}
			return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateStageInput, SurfaceSessionID: action.SurfaceSessionID, InstanceID: instance.InstanceID, ThreadCWD: instance.WorkspaceRoot, Text: string(raw)}}}, nil
		}
		// The legacy detached path performs no staging; it references the
		// already-admitted local attachments directly until a workspace exists.
		if action.Kind == control.ActionTextMessage {
			if prompt := inboundFileAttachmentPrompt(action.Files); prompt != "" {
				action.Inputs = append([]agentproto.Input{{Type: agentproto.InputText, Text: prompt}}, action.Inputs...)
			}
		}
	}
	if action.Kind == control.ActionWorkspaceNewWorktree || (strings.HasPrefix(string(action.Kind), "surface.target_picker.") && app.Service().PrivateTargetPickerUsesGitPreview(action.SurfaceSessionID)) {
		raw, err := json.Marshal(action)
		if err != nil {
			return nil, err
		}
		return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateWorktreeObserve, SurfaceSessionID: action.SurfaceSessionID, Text: string(raw)}}}, nil
	}
	events, err := app.Action(action)
	if err != nil {
		return nil, err
	}
	return r.withHistoricalRecovery(app, action, events)
}

func (r privateHostReducer) withHistoricalRecovery(app *privatechat.App, action control.Action, events []eventcontract.Event) ([]eventcontract.Event, error) {
	hasSnapshot := false
	for _, event := range events {
		if event.Snapshot != nil {
			hasSnapshot = true
			break
		}
	}
	if !hasSnapshot {
		return events, nil
	}
	if r.host.applications == nil || r.host.applications.journal == nil {
		return events, nil
	}
	owner, generation, ok := app.OwnerGeneration(action.SurfaceSessionID)
	if !ok || owner.GatewayID != action.GatewayID || owner.ChatID != action.ChatID || owner.UserID != action.ActorUserID {
		return nil, privatechat.ErrInvalidOwner
	}
	summary, err := r.host.applications.journal.MigrationRecoverySummary(context.Background(), owner, generation)
	if err != nil {
		return nil, err
	}
	if summary.UnconfirmedReplies == 0 && summary.ClaimedInputs == 0 && summary.OtherInputs == 0 && summary.SuspendedInputs == 0 && summary.UnresolvedExecutions == 0 {
		return events, nil
	}
	for i := range events {
		if events[i].Snapshot != nil {
			snapshot := *events[i].Snapshot
			snapshot.HistoricalRecovery = &control.HistoricalRecoverySummary{UnconfirmedReplies: summary.UnconfirmedReplies, ClaimedInputs: summary.ClaimedInputs, OtherInputs: summary.OtherInputs, SuspendedInputs: summary.SuspendedInputs, UnresolvedExecutions: summary.UnresolvedExecutions}
			events[i].Snapshot = &snapshot
			events[i].Payload = nil
		}
	}
	return events, nil
}

// Bot-level mutations wait for their durable config-owner intent and receipt;
// viewing a settings page remains available while that port is being wired.
func privateSettingsWriteNeedsOwnerReceipt(action control.Action) bool {
	switch action.Kind {
	case control.ActionModeCommand, control.ActionModelCommand, control.ActionReasoningCommand, control.ActionAccessCommand, control.ActionCodexProfileCommand, control.ActionClaudeProfileCommand, control.ActionOpenCodeProfileCommand:
		return len(strings.Fields(action.Text)) > 1 || strings.TrimSpace(action.OptionID) != ""
	default:
		return false
	}
}

func (r privateHostReducer) ReduceObservation(app *privatechat.App, action control.Action) ([]eventcontract.Event, error) {
	if err := r.check(app); err != nil {
		return nil, err
	}
	if err := app.ValidateObservation(action); err != nil {
		return nil, err
	}
	action = r.classify(action)
	if flow := app.Service().PrivateUpgradeFlow(action.SurfaceSessionID); flow != nil && (flow.Phase == "running" || flow.Phase == "cancelling" || flow.Phase == "restarting") && action.Kind == control.ActionReactionCreated {
		return nil, privatechat.ErrDeferred
	}
	if notice := rejectedInboundNotice(action); notice != nil {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: notice}}, nil
	}
	return app.Observe(action)
}

func (r privateHostReducer) PlanCard(app *privatechat.App, action control.Action, events []eventcontract.Event) (*privatechat.CardResponse, []eventcontract.Event, error) {
	if err := r.check(app); err != nil {
		return nil, nil, err
	}
	if action.Inbound == nil || !action.Inbound.CardCallback || strings.TrimSpace(action.Inbound.OpenMessageID) == "" || !control.SupportsFeishuSynchronousCurrentCardReplacement(action) {
		return nil, events, nil
	}
	if rejectedInboundNotice(r.classify(action)) != nil {
		return nil, events, nil
	}
	contract := control.ResolveFeishuFrontstageActionContract(action)
	if contract.CurrentCardMode == control.FeishuFrontstageCurrentCardNone {
		return nil, events, nil
	}
	if contract.CurrentCardMode == control.FeishuFrontstageCurrentCardInlineView && inlineFallbackReplacementBlockedByActivePicker(events) {
		return nil, events, nil
	}
	return r.planLocalCard(action, contract, events)
}

func (r privateHostReducer) classify(action control.Action) control.Action {
	action = r.host.classifyInboundAction(action)
	if inboundVerdict(action) == control.InboundLifecycleOldCard && inboundReason(action) == "card_lifecycle_mismatch" && r.host.service.PrivateRestoredPickerCardMatches(action) {
		action.Inbound.LifecycleVerdict = control.InboundLifecycleCurrent
		action.Inbound.LifecycleReason = ""
	}
	return action
}
