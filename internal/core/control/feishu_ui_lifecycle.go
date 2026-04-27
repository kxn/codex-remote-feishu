package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/handoffcontract"
)

const (
	FeishuUIInlineReplaceFreshnessDaemonLifecycle = "daemon_lifecycle"
	FeishuUIInlineReplaceViewSessionSurfaceState  = "surface_state_rederived"
	FeishuUIInlineReplaceOwnerController          = "feishu_ui_controller"
)

type FeishuFrontstageCurrentCardMode string

const (
	FeishuFrontstageCurrentCardNone            FeishuFrontstageCurrentCardMode = ""
	FeishuFrontstageCurrentCardInlineView      FeishuFrontstageCurrentCardMode = "inline_view"
	FeishuFrontstageCurrentCardFirstResultCard FeishuFrontstageCurrentCardMode = "first_result_card"
)

type FeishuFrontstageLauncherDisposition string

const (
	FeishuFrontstageLauncherKeep          FeishuFrontstageLauncherDisposition = "keep"
	FeishuFrontstageLauncherEnterOwner    FeishuFrontstageLauncherDisposition = "enter_owner"
	FeishuFrontstageLauncherEnterTerminal FeishuFrontstageLauncherDisposition = "enter_terminal"
)

type FeishuFollowupHandoffClass = handoffcontract.HandoffClass

const (
	FeishuFollowupHandoffClassNotice          = handoffcontract.HandoffClassNotice
	FeishuFollowupHandoffClassThreadSelection = handoffcontract.HandoffClassThreadSelection
	FeishuFollowupHandoffClassNavigation      = handoffcontract.HandoffClassNavigation
	FeishuFollowupHandoffClassProcessDetail   = handoffcontract.HandoffClassProcessDetail
	FeishuFollowupHandoffClassTerminal        = handoffcontract.HandoffClassTerminalContent
)

type FeishuFollowupPolicy = handoffcontract.FollowupPolicy

// FeishuFrontstageActionContract is the single action-level contract that
// decides whether the current stamped card may be replaced synchronously and
// whether an existing launcher flow should stay alive or hand off.
type FeishuFrontstageActionContract struct {
	CurrentCardMode           FeishuFrontstageCurrentCardMode
	LauncherDisposition       FeishuFrontstageLauncherDisposition
	RequiresDaemonFreshness   bool
	DaemonFreshness           string
	RequiresViewSession       bool
	ViewSessionStrategy       string
	ContinuationDaemonCommand DaemonCommandKind
	FollowupPolicy            FeishuFollowupPolicy
}

func ResolveFeishuFrontstageActionContract(action Action) FeishuFrontstageActionContract {
	binding, hasBinding := ResolveFeishuCommandBindingFromAction(action)
	contract := FeishuFrontstageActionContract{
		LauncherDisposition:     ResolveFeishuLauncherDisposition(action),
		RequiresDaemonFreshness: true,
		DaemonFreshness:         FeishuUIInlineReplaceFreshnessDaemonLifecycle,
		RequiresViewSession:     false,
		ViewSessionStrategy:     FeishuUIInlineReplaceViewSessionSurfaceState,
	}

	switch {
	case inlineReplaceableFeishuUIIntentAction(action):
		contract.CurrentCardMode = FeishuFrontstageCurrentCardInlineView
		return contract
	case firstResultCardReplaceableAction(action):
		contract.CurrentCardMode = FeishuFrontstageCurrentCardFirstResultCard
	}

	if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard && hasBinding && binding.ContinuationDaemonCommand != "" {
		contract.ContinuationDaemonCommand = binding.ContinuationDaemonCommand
	}
	if action.Kind == ActionVSCodeMigrate && contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
		contract.ContinuationDaemonCommand = DaemonCommandVSCodeMigrate
	}

	if hasBinding && !binding.FollowupPolicy.Empty() {
		contract.FollowupPolicy = binding.FollowupPolicy
	}
	if action.Kind == ActionAttachInstance {
		contract.FollowupPolicy = FeishuFollowupPolicy{
			DropClasses: []FeishuFollowupHandoffClass{
				FeishuFollowupHandoffClassThreadSelection,
			},
		}
	}
	contract.FollowupPolicy = contract.FollowupPolicy.Normalized()

	return contract
}

func ResolveFeishuLauncherDisposition(action Action) FeishuFrontstageLauncherDisposition {
	if binding, ok := ResolveFeishuCommandBindingFromAction(action); ok && binding.LauncherDisposition != "" {
		return binding.LauncherDisposition
	}
	return FeishuFrontstageLauncherEnterOwner
}

func ActionTargetsCurrentFeishuCard(action Action) bool {
	return action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""
}

func SupportsFeishuSynchronousCurrentCardReplacement(action Action) bool {
	contract := ResolveFeishuFrontstageActionContract(action)
	if contract.CurrentCardMode == FeishuFrontstageCurrentCardNone {
		return false
	}
	if !contract.RequiresDaemonFreshness {
		return true
	}
	return ActionTargetsCurrentFeishuCard(action)
}

func AllowsInlineCardReplacement(action Action) bool {
	contract := ResolveFeishuFrontstageActionContract(action)
	if contract.CurrentCardMode != FeishuFrontstageCurrentCardInlineView {
		return false
	}
	return SupportsFeishuSynchronousCurrentCardReplacement(action)
}

func AllowsCommandCardResultReplacement(action Action) bool {
	if !ActionTargetsCurrentFeishuCard(action) {
		return false
	}
	switch action.Kind {
	case ActionListInstances,
		ActionShowThreads,
		ActionShowAllThreads,
		ActionReviewStart,
		ActionReviewDiscard,
		ActionReviewApply,
		ActionAttachInstance,
		ActionUseThread,
		ActionShowCommandHelp,
		ActionStatus,
		ActionTurnPatchCommand,
		ActionTurnPatchRollback,
		ActionStop,
		ActionNewThread,
		ActionFollowLocal,
		ActionDetach,
		ActionVSCodeMigrate:
		return true
	default:
		return firstResultCardReplaceableAction(action)
	}
}

func inlineReplaceableFeishuUIIntentAction(action Action) bool {
	if binding, ok := ResolveFeishuCommandBindingFromAction(action); ok {
		if binding.Kind == FeishuCommandBindingConfigFlow && !isSingleTokenSlashCommand(action.Text) {
			return true
		}
	}
	switch action.Kind {
	case ActionPlanProposalDecision,
		ActionRespondRequest,
		ActionControlRequest:
		if isSingleTokenSlashCommand(action.Text) {
			break
		}
		return true
	}
	intent, ok := FeishuUIIntentFromAction(action)
	if !ok || intent == nil {
		return false
	}
	if binding, ok := ResolveFeishuCommandBindingFromAction(action); ok {
		switch binding.Kind {
		case FeishuCommandBindingConfigFlow,
			FeishuCommandBindingWorkspaceSession,
			FeishuCommandBindingInlinePage:
			return true
		}
	}
	switch intent.Kind {
	case FeishuUIIntentShowRecentWorkspaces,
		FeishuUIIntentShowAllWorkspaces,
		FeishuUIIntentShowScopedThreads,
		FeishuUIIntentShowWorkspaceThreads,
		FeishuUIIntentShowAllThreadWorkspaces,
		FeishuUIIntentShowRecentThreadWorkspaces,
		FeishuUIIntentThreadSelectionPage,
		FeishuUIIntentPathPickerEnter,
		FeishuUIIntentPathPickerUp,
		FeishuUIIntentPathPickerSelect,
		FeishuUIIntentPathPickerPage,
		FeishuUIIntentTargetPickerSelectMode,
		FeishuUIIntentTargetPickerSelectSource,
		FeishuUIIntentTargetPickerSelectWorkspace,
		FeishuUIIntentTargetPickerSelectSession,
		FeishuUIIntentTargetPickerPage,
		FeishuUIIntentTargetPickerOpenPathPicker,
		FeishuUIIntentTargetPickerBack,
		FeishuUIIntentTargetPickerCancel,
		FeishuUIIntentHistoryPage,
		FeishuUIIntentHistoryDetail:
		return true
	default:
		return false
	}
}

func firstResultCardReplaceableAction(action Action) bool {
	switch action.Kind {
	case ActionListInstances,
		ActionShowThreads,
		ActionShowAllThreads,
		ActionReviewStart,
		ActionReviewDiscard,
		ActionReviewApply,
		ActionAttachInstance,
		ActionUseThread,
		ActionShowCommandHelp,
		ActionStatus,
		ActionStop,
		ActionNewThread,
		ActionFollowLocal,
		ActionDetach,
		ActionVSCodeMigrate:
		return true
	case ActionCronCommand:
		return !cronCommandRunsImmediately(action.Text)
	case ActionUpgradeCommand:
		return !FeishuUpgradeCommandRunsImmediately(action.Text)
	case ActionDebugCommand:
		return !debugCommandRunsImmediately(action.Text)
	case ActionVSCodeMigrateCommand:
		return isSingleTokenSlashCommand(action.Text)
	default:
		return false
	}
}

func cronCommandRunsImmediately(text string) bool {
	fields := normalizedCommandFields(text)
	if len(fields) == 0 || fields[0] != "/cron" {
		return false
	}
	switch {
	case len(fields) == 2 && (fields[1] == "reload" || fields[1] == "repair"):
		return true
	case len(fields) == 3 && fields[1] == "run" && strings.TrimSpace(fields[2]) != "":
		return true
	default:
		return false
	}
}

func debugCommandRunsImmediately(text string) bool {
	fields := normalizedCommandFields(text)
	if len(fields) == 0 || fields[0] != "/debug" {
		return false
	}
	switch {
	case len(fields) == 2 && (fields[1] == "admin" || fields[1] == "upgrade"):
		return true
	case len(fields) == 3 && fields[1] == "track" && isReleaseTrackToken(fields[2]):
		return true
	default:
		return false
	}
}

func normalizedCommandFields(text string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	return fields
}

func isReleaseTrackToken(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "alpha", "beta", "production":
		return true
	default:
		return false
	}
}

func isSingleTokenSlashCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) == 1 && strings.HasPrefix(fields[0], "/")
}
