package control

import "strings"

const (
	FeishuUIInlineReplaceFreshnessDaemonLifecycle = "daemon_lifecycle"
	FeishuUIInlineReplaceViewSessionSurfaceState  = "surface_state_rederived"
	FeishuUIInlineReplaceOwnerController          = "feishu_ui_controller"
)

// FeishuUIInlineReplacePolicy makes the current inline-replace lifecycle
// strategy explicit without changing user-visible behavior.
type FeishuUIInlineReplacePolicy struct {
	Owner                   string
	ReplaceCurrentCard      bool
	RequiresDaemonFreshness bool
	DaemonFreshness         string
	RequiresViewSession     bool
	ViewSessionStrategy     string
}

func InlineCardReplacementPolicy(action Action) (FeishuUIInlineReplacePolicy, bool) {
	if !inlineReplaceableFeishuUIIntentAction(action) {
		return FeishuUIInlineReplacePolicy{}, false
	}
	return FeishuUIInlineReplacePolicy{
		Owner:                   FeishuUIInlineReplaceOwnerController,
		ReplaceCurrentCard:      true,
		RequiresDaemonFreshness: true,
		DaemonFreshness:         FeishuUIInlineReplaceFreshnessDaemonLifecycle,
		RequiresViewSession:     false,
		ViewSessionStrategy:     FeishuUIInlineReplaceViewSessionSurfaceState,
	}, true
}

func inlineReplaceableFeishuUIIntentAction(action Action) bool {
	intent, ok := FeishuUIIntentFromAction(action)
	if !ok || intent == nil {
		return false
	}
	switch intent.Kind {
	case FeishuUIIntentShowCommandMenu,
		FeishuUIIntentShowHistory,
		FeishuUIIntentShowModeCatalog,
		FeishuUIIntentShowAutoContinueCatalog,
		FeishuUIIntentShowReasoningCatalog,
		FeishuUIIntentShowAccessCatalog,
		FeishuUIIntentShowModelCatalog,
		FeishuUIIntentShowVerboseCatalog,
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
		FeishuUIIntentTargetPickerCancel,
		FeishuUIIntentHistoryPage,
		FeishuUIIntentHistoryDetail:
		return true
	default:
		return false
	}
}

func AllowsInlineCardReplacement(action Action) bool {
	policy, ok := InlineCardReplacementPolicy(action)
	if !ok || !policy.ReplaceCurrentCard {
		return false
	}
	if !policy.RequiresDaemonFreshness {
		return true
	}
	return action.Inbound != nil && strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) != ""
}

// AllowsCommandSubmissionAnchorReplacement returns whether this card-triggered
// command should synchronously return a lightweight "已提交" replacement card
// while keeping command results append-only.
func AllowsCommandSubmissionAnchorReplacement(action Action) bool {
	if AllowsInlineCardReplacement(action) {
		return false
	}
	if action.Inbound == nil || strings.TrimSpace(action.Inbound.CardDaemonLifecycleID) == "" {
		return false
	}
	switch action.Kind {
	case ActionListInstances,
		ActionShowThreads,
		ActionShowAllThreads,
		ActionStatus,
		ActionStop,
		ActionSteerAll,
		ActionNewThread,
		ActionFollowLocal,
		ActionDetach:
		return true
	case ActionUpgradeCommand, ActionDebugCommand, ActionCronCommand:
		return isSingleTokenSlashCommand(action.Text)
	default:
		return false
	}
}

func isSingleTokenSlashCommand(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	return len(fields) == 1 && strings.HasPrefix(fields[0], "/")
}
