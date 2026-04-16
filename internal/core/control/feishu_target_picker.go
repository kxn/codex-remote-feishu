package control

import "time"

type TargetPickerRequestSource string

const (
	TargetPickerRequestSourceList      TargetPickerRequestSource = "list"
	TargetPickerRequestSourceUse       TargetPickerRequestSource = "use"
	TargetPickerRequestSourceUseAll    TargetPickerRequestSource = "useall"
	TargetPickerRequestSourceWorkspace TargetPickerRequestSource = "workspace"
)

type FeishuTargetPickerSessionKind string

const (
	FeishuTargetPickerSessionThread    FeishuTargetPickerSessionKind = "thread"
	FeishuTargetPickerSessionNewThread FeishuTargetPickerSessionKind = "new_thread"
)

type FeishuTargetPickerMode string

const (
	FeishuTargetPickerModeExistingWorkspace FeishuTargetPickerMode = "existing_workspace"
	FeishuTargetPickerModeAddWorkspace      FeishuTargetPickerMode = "add_workspace"
)

type FeishuTargetPickerSourceKind string

const (
	FeishuTargetPickerSourceLocalDirectory FeishuTargetPickerSourceKind = "local_directory"
	FeishuTargetPickerSourceGitURL         FeishuTargetPickerSourceKind = "git_url"
)

// FeishuTargetPickerView is the UI-owned read model for the unified
// workspace/session target picker card.
type FeishuTargetPickerView struct {
	PickerID               string
	Title                  string
	Source                 TargetPickerRequestSource
	SelectedMode           FeishuTargetPickerMode
	SelectedSource         FeishuTargetPickerSourceKind
	ShowModeSwitch         bool
	ShowWorkspaceSelect    bool
	ShowSessionSelect      bool
	ShowSourceSelect       bool
	ModePlaceholder        string
	WorkspacePlaceholder   string
	SessionPlaceholder     string
	SourcePlaceholder      string
	SelectedWorkspaceKey   string
	SelectedSessionValue   string
	SelectedWorkspaceLabel string
	SelectedWorkspaceMeta  string
	SelectedSessionLabel   string
	SelectedSessionMeta    string
	ConfirmLabel           string
	CanConfirm             bool
	Hint                   string
	ModeOptions            []FeishuTargetPickerModeOption
	WorkspaceOptions       []FeishuTargetPickerWorkspaceOption
	SessionOptions         []FeishuTargetPickerSessionOption
	SourceOptions          []FeishuTargetPickerSourceOption
	AddModeSummary         string
	AddModeDetail          string
	SourceUnavailableHint  string
}

type FeishuTargetPickerModeOption struct {
	Value    FeishuTargetPickerMode
	Label    string
	Selected bool
}

type FeishuTargetPickerWorkspaceOption struct {
	Value           string
	Label           string
	MetaText        string
	RecoverableOnly bool
	Synthetic       bool
}

type FeishuTargetPickerSessionOption struct {
	Value    string
	Kind     FeishuTargetPickerSessionKind
	Label    string
	MetaText string
}

type FeishuTargetPickerSourceOption struct {
	Value             FeishuTargetPickerSourceKind
	Label             string
	MetaText          string
	Available         bool
	UnavailableReason string
}

type TargetPickerResult struct {
	PickerID     string
	Source       TargetPickerRequestSource
	Mode         FeishuTargetPickerMode
	SourceKind   FeishuTargetPickerSourceKind
	WorkspaceKey string
	SessionValue string
	OwnerUserID  string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}
