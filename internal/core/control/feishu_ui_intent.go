package control

type FeishuUIIntentKind string

const (
	FeishuUIIntentShowCommandMenu            FeishuUIIntentKind = "show_command_menu"
	FeishuUIIntentShowModeCatalog            FeishuUIIntentKind = "show_mode_catalog"
	FeishuUIIntentShowAutoContinueCatalog    FeishuUIIntentKind = "show_auto_continue_catalog"
	FeishuUIIntentShowReasoningCatalog       FeishuUIIntentKind = "show_reasoning_catalog"
	FeishuUIIntentShowAccessCatalog          FeishuUIIntentKind = "show_access_catalog"
	FeishuUIIntentShowModelCatalog           FeishuUIIntentKind = "show_model_catalog"
	FeishuUIIntentShowRecentWorkspaces       FeishuUIIntentKind = "show_recent_workspaces"
	FeishuUIIntentShowAllWorkspaces          FeishuUIIntentKind = "show_all_workspaces"
	FeishuUIIntentShowThreads                FeishuUIIntentKind = "show_threads"
	FeishuUIIntentShowAllThreads             FeishuUIIntentKind = "show_all_threads"
	FeishuUIIntentShowScopedThreads          FeishuUIIntentKind = "show_scoped_threads"
	FeishuUIIntentShowWorkspaceThreads       FeishuUIIntentKind = "show_workspace_threads"
	FeishuUIIntentShowAllThreadWorkspaces    FeishuUIIntentKind = "show_all_thread_workspaces"
	FeishuUIIntentShowRecentThreadWorkspaces FeishuUIIntentKind = "show_recent_thread_workspaces"
)

// FeishuUIIntent classifies same-context Feishu navigation handled by the
// Feishu UI controller instead of the main product reducer.
type FeishuUIIntent struct {
	Kind         FeishuUIIntentKind
	RawText      string
	WorkspaceKey string
}

func FeishuUIIntentFromAction(action Action) (*FeishuUIIntent, bool) {
	switch action.Kind {
	case ActionShowCommandMenu:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowCommandMenu, RawText: action.Text}, true
	case ActionModeCommand:
		if SupportsInlineCardReplacement(action) {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowModeCatalog, RawText: action.Text}, true
		}
	case ActionAutoContinueCommand:
		if SupportsInlineCardReplacement(action) {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowAutoContinueCatalog, RawText: action.Text}, true
		}
	case ActionReasoningCommand:
		if SupportsInlineCardReplacement(action) {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowReasoningCatalog, RawText: action.Text}, true
		}
	case ActionAccessCommand:
		if SupportsInlineCardReplacement(action) {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowAccessCatalog, RawText: action.Text}, true
		}
	case ActionModelCommand:
		if SupportsInlineCardReplacement(action) {
			return &FeishuUIIntent{Kind: FeishuUIIntentShowModelCatalog, RawText: action.Text}, true
		}
	case ActionShowRecentWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentWorkspaces}, true
	case ActionShowAllWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllWorkspaces}, true
	case ActionShowThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowThreads}, true
	case ActionShowAllThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreads}, true
	case ActionShowScopedThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowScopedThreads}, true
	case ActionShowWorkspaceThreads:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowWorkspaceThreads, WorkspaceKey: action.WorkspaceKey}, true
	case ActionShowAllThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowAllThreadWorkspaces}, true
	case ActionShowRecentThreadWorkspaces:
		return &FeishuUIIntent{Kind: FeishuUIIntentShowRecentThreadWorkspaces}, true
	}
	return nil, false
}
