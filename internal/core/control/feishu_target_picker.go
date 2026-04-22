package control

import "time"

type TargetPickerRequestSource string

const (
	TargetPickerRequestSourceList      TargetPickerRequestSource = "list"
	TargetPickerRequestSourceUse       TargetPickerRequestSource = "use"
	TargetPickerRequestSourceUseAll    TargetPickerRequestSource = "useall"
	TargetPickerRequestSourceWorkspace TargetPickerRequestSource = "workspace"
	TargetPickerRequestSourceDir       TargetPickerRequestSource = "workspace_new_dir"
	TargetPickerRequestSourceGit       TargetPickerRequestSource = "workspace_new_git"
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

type FeishuTargetPickerPage string

const (
	FeishuTargetPickerPageMode           FeishuTargetPickerPage = "mode"
	FeishuTargetPickerPageTarget         FeishuTargetPickerPage = "target"
	FeishuTargetPickerPageSource         FeishuTargetPickerPage = "source"
	FeishuTargetPickerPageLocalDirectory FeishuTargetPickerPage = "local_directory"
	FeishuTargetPickerPageGit            FeishuTargetPickerPage = "git"
)

type FeishuTargetPickerStage string

const (
	FeishuTargetPickerStageEditing    FeishuTargetPickerStage = "editing"
	FeishuTargetPickerStageProcessing FeishuTargetPickerStage = "processing"
	FeishuTargetPickerStageSucceeded  FeishuTargetPickerStage = "succeeded"
	FeishuTargetPickerStageFailed     FeishuTargetPickerStage = "failed"
	FeishuTargetPickerStageCancelled  FeishuTargetPickerStage = "cancelled"
)

const (
	FeishuTargetPickerPathFieldLocalDirectory   = "local_directory"
	FeishuTargetPickerPathFieldGitParentDir     = "git_parent_dir"
	FeishuTargetPickerGitRepoURLFieldName       = "target_picker_git_repo_url"
	FeishuTargetPickerGitDirectoryNameFieldName = "target_picker_git_directory_name"
)

type FeishuTargetPickerMessageLevel string

const (
	FeishuTargetPickerMessageInfo    FeishuTargetPickerMessageLevel = "info"
	FeishuTargetPickerMessageWarning FeishuTargetPickerMessageLevel = "warning"
	FeishuTargetPickerMessageDanger  FeishuTargetPickerMessageLevel = "danger"
)

// FeishuTargetPickerView is the UI-owned read model for the unified
// workspace/session target picker card.
type FeishuTargetPickerView struct {
	PickerID               string
	MessageID              string
	Title                  string
	Source                 TargetPickerRequestSource
	Stage                  FeishuTargetPickerStage
	Page                   FeishuTargetPickerPage
	StageLabel             string
	Question               string
	BodySections           []FeishuCardTextSection
	NoticeSections         []FeishuCardTextSection
	Sealed                 bool
	StatusTitle            string
	StatusText             string
	StatusSections         []FeishuCardTextSection
	StatusFooter           string
	CanCancelProcessing    bool
	ProcessingCancelLabel  string
	CanGoBack              bool
	BackLabel              string
	BackCommandText        string
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
	LocalDirectoryPath     string
	GitParentDir           string
	GitRepoURL             string
	GitDirectoryName       string
	GitFinalPath           string
	Messages               []FeishuTargetPickerMessage
	SourceMessages         []FeishuTargetPickerMessage
}

type FeishuTargetPickerModeOption struct {
	Value             FeishuTargetPickerMode
	Label             string
	MetaText          string
	Selected          bool
	Available         bool
	UnavailableReason string
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

type FeishuTargetPickerMessage struct {
	Level FeishuTargetPickerMessageLevel
	Text  string
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
