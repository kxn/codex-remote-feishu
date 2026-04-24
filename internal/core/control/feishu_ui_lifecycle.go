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

// FeishuUIInlineReplacePolicy remains as a compatibility view for tests and
// legacy callers that still consume the inline-specific policy shape.
type FeishuUIInlineReplacePolicy struct {
	Owner                   string
	ReplaceCurrentCard      bool
	RequiresDaemonFreshness bool
	DaemonFreshness         string
	RequiresViewSession     bool
	ViewSessionStrategy     string
}

func ResolveFeishuFrontstageActionContract(action Action) FeishuFrontstageActionContract {
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

	switch action.Kind {
	case ActionUpgradeCommand:
		if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
			contract.ContinuationDaemonCommand = DaemonCommandUpgrade
		}
	case ActionDebugCommand:
		if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
			contract.ContinuationDaemonCommand = DaemonCommandDebug
		}
	case ActionCronCommand:
		if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
			contract.ContinuationDaemonCommand = DaemonCommandCron
		}
	case ActionVSCodeMigrateCommand:
		if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
			contract.ContinuationDaemonCommand = DaemonCommandVSCodeMigrateCommand
		}
	case ActionVSCodeMigrate:
		if contract.CurrentCardMode == FeishuFrontstageCurrentCardFirstResultCard {
			contract.ContinuationDaemonCommand = DaemonCommandVSCodeMigrate
		}
	}

	switch action.Kind {
	case ActionShowCommandHelp, ActionStatus, ActionStop, ActionNewThread, ActionFollowLocal, ActionDetach:
		contract.FollowupPolicy = FeishuFollowupPolicy{
			DropClasses: []FeishuFollowupHandoffClass{
				FeishuFollowupHandoffClassNotice,
				FeishuFollowupHandoffClassThreadSelection,
			},
		}
	case ActionAttachInstance:
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
	switch action.Kind {
	case ActionShowCommandMenu,
		ActionModeCommand,
		ActionAutoWhipCommand,
		ActionAutoContinueCommand,
		ActionReasoningCommand,
		ActionAccessCommand,
		ActionPlanCommand,
		ActionModelCommand,
		ActionVerboseCommand:
		return FeishuFrontstageLauncherKeep
	case ActionShowCommandHelp, ActionStatus:
		return FeishuFrontstageLauncherEnterTerminal
	default:
		return FeishuFrontstageLauncherEnterOwner
	}
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

func InlineCardReplacementPolicy(action Action) (FeishuUIInlineReplacePolicy, bool) {
	contract := ResolveFeishuFrontstageActionContract(action)
	if contract.CurrentCardMode != FeishuFrontstageCurrentCardInlineView {
		return FeishuUIInlineReplacePolicy{}, false
	}
	return FeishuUIInlineReplacePolicy{
		Owner:                   FeishuUIInlineReplaceOwnerController,
		ReplaceCurrentCard:      true,
		RequiresDaemonFreshness: contract.RequiresDaemonFreshness,
		DaemonFreshness:         contract.DaemonFreshness,
		RequiresViewSession:     contract.RequiresViewSession,
		ViewSessionStrategy:     contract.ViewSessionStrategy,
	}, true
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
	default:
		return firstResultCardReplaceableAction(action)
	}
}

func AllowsBareCommandContinuation(Action) bool { return false }

func AllowsCommandSubmissionAnchorReplacement(Action) bool { return false }

func inlineReplaceableFeishuUIIntentAction(action Action) bool {
	switch action.Kind {
	case ActionModeCommand,
		ActionAutoWhipCommand,
		ActionAutoContinueCommand,
		ActionReasoningCommand,
		ActionAccessCommand,
		ActionPlanCommand,
		ActionModelCommand,
		ActionVerboseCommand,
		ActionPlanProposalDecision,
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
	switch intent.Kind {
	case FeishuUIIntentShowCommandMenu,
		FeishuUIIntentShowWorkspaceRoot,
		FeishuUIIntentShowWorkspaceList,
		FeishuUIIntentShowWorkspaceNew,
		FeishuUIIntentShowWorkspaceNewDir,
		FeishuUIIntentShowWorkspaceNewGit,
		FeishuUIIntentShowHistory,
		FeishuUIIntentShowModeCatalog,
		FeishuUIIntentShowAutoWhipCatalog,
		FeishuUIIntentShowAutoContinueCatalog,
		FeishuUIIntentShowReasoningCatalog,
		FeishuUIIntentShowAccessCatalog,
		FeishuUIIntentShowPlanCatalog,
		FeishuUIIntentShowModelCatalog,
		FeishuUIIntentShowVerboseCatalog,
		FeishuUIIntentShowList,
		FeishuUIIntentOpenSendFilePicker,
		FeishuUIIntentShowRecentWorkspaces,
		FeishuUIIntentShowAllWorkspaces,
		FeishuUIIntentShowThreads,
		FeishuUIIntentShowAllThreads,
		FeishuUIIntentShowScopedThreads,
		FeishuUIIntentShowWorkspaceThreads,
		FeishuUIIntentShowAllThreadWorkspaces,
		FeishuUIIntentShowRecentThreadWorkspaces,
		FeishuUIIntentPathPickerEnter,
		FeishuUIIntentPathPickerUp,
		FeishuUIIntentPathPickerSelect,
		FeishuUIIntentTargetPickerSelectMode,
		FeishuUIIntentTargetPickerSelectSource,
		FeishuUIIntentTargetPickerSelectWorkspace,
		FeishuUIIntentTargetPickerSelectSession,
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
		return !upgradeCommandRunsImmediately(action.Text)
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

func upgradeCommandRunsImmediately(text string) bool {
	fields := normalizedCommandFields(text)
	if len(fields) == 0 || fields[0] != "/upgrade" {
		return false
	}
	switch {
	case len(fields) == 2 && (fields[1] == "latest" || fields[1] == "codex" || fields[1] == "dev" || fields[1] == "local"):
		return true
	case len(fields) == 3 && fields[1] == "track" && isReleaseTrackToken(fields[2]):
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
