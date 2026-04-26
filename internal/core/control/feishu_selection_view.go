package control

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

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
	PromptKind       SelectionPromptKind
	CatalogFamilyID  string
	CatalogVariantID string
	CatalogBackend   agentproto.Backend
	Instance         *FeishuInstanceSelectionView
	Workspace        *FeishuWorkspaceSelectionView
	Thread           *FeishuThreadSelectionView
	KickThread       *FeishuKickThreadSelectionView
}

type FeishuInstanceSelectionView struct {
	Current *FeishuInstanceSelectionCurrent
	Entries []FeishuInstanceSelectionEntry
}

type FeishuInstanceSelectionCurrent struct {
	InstanceID  string
	Label       string
	ContextText string
}

type FeishuInstanceSelectionEntry struct {
	InstanceID   string
	Label        string
	MetaText     string
	ButtonLabel  string
	HasFocus     bool
	Disabled     bool
	LatestUsedAt string
}

type FeishuWorkspaceSelectionView struct {
	Page       int
	PageSize   int
	TotalPages int
	Current    *FeishuWorkspaceSelectionCurrent
	Entries    []FeishuWorkspaceSelectionEntry
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
	Page             int
	PageSize         int
	TotalPages       int
	ReturnPage       int
	RecentLimit      int
	Cursor           int
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

type FeishuKickThreadSelectionView struct {
	ThreadID       string
	ThreadLabel    string
	ThreadSubtitle string
	Hint           string
	CancelLabel    string
	ConfirmLabel   string
}
