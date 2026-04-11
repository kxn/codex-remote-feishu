package control

type FeishuThreadSelectionViewMode string

const (
	FeishuThreadSelectionNormalGlobalRecent  FeishuThreadSelectionViewMode = "normal_global_recent"
	FeishuThreadSelectionNormalGlobalAll     FeishuThreadSelectionViewMode = "normal_global_all"
	FeishuThreadSelectionNormalScopedRecent  FeishuThreadSelectionViewMode = "normal_scoped_recent"
	FeishuThreadSelectionNormalScopedAll     FeishuThreadSelectionViewMode = "normal_scoped_all"
	FeishuThreadSelectionNormalWorkspaceView FeishuThreadSelectionViewMode = "normal_workspace_view"
	FeishuThreadSelectionVSCodeRecent        FeishuThreadSelectionViewMode = "vscode_recent"
	FeishuThreadSelectionVSCodeAll           FeishuThreadSelectionViewMode = "vscode_all"
	FeishuThreadSelectionVSCodeScopedAll     FeishuThreadSelectionViewMode = "vscode_scoped_all"
)

// FeishuSelectionView is the UI-owned selection view payload used by the
// Feishu adapter for workspace/thread selection cards.
type FeishuSelectionView struct {
	PromptKind SelectionPromptKind
	Workspace  *FeishuWorkspaceSelectionView
	Thread     *FeishuThreadSelectionView
}

type FeishuWorkspaceSelectionView struct {
	Expanded    bool
	RecentLimit int
	Current     *FeishuWorkspaceSelectionCurrent
	Entries     []FeishuWorkspaceSelectionEntry
}

type FeishuWorkspaceSelectionCurrent struct {
	WorkspaceKey   string
	WorkspaceLabel string
	AgeText        string
}

type FeishuWorkspaceSelectionEntry struct {
	WorkspaceKey      string
	WorkspaceLabel    string
	AgeText           string
	HasVSCodeActivity bool
	Busy              bool
	Attachable        bool
	RecoverableOnly   bool
}

type FeishuThreadSelectionView struct {
	Mode             FeishuThreadSelectionViewMode
	RecentLimit      int
	CurrentWorkspace *FeishuThreadSelectionWorkspaceContext
	CurrentInstance  *FeishuThreadSelectionInstanceContext
	Workspace        *FeishuThreadSelectionWorkspaceContext
	Entries          []FeishuThreadSelectionEntry
}

type FeishuThreadSelectionWorkspaceContext struct {
	WorkspaceKey   string
	WorkspaceLabel string
	AgeText        string
}

type FeishuThreadSelectionInstanceContext struct {
	Label  string
	Status string
}

type FeishuThreadSelectionEntry struct {
	ThreadID            string
	Summary             string
	WorkspaceKey        string
	WorkspaceLabel      string
	AgeText             string
	Status              string
	VSCodeFocused       bool
	Disabled            bool
	AllowCrossWorkspace bool
	Current             bool
}
