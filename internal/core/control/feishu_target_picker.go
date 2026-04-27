package control

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

type TargetPickerRequestSource string

const (
	TargetPickerRequestSourceList      TargetPickerRequestSource = "list"
	TargetPickerRequestSourceUse       TargetPickerRequestSource = "use"
	TargetPickerRequestSourceUseAll    TargetPickerRequestSource = "useall"
	TargetPickerRequestSourceWorkspace TargetPickerRequestSource = "workspace"
	TargetPickerRequestSourceDir       TargetPickerRequestSource = "workspace_new_dir"
	TargetPickerRequestSourceGit       TargetPickerRequestSource = "workspace_new_git"
	TargetPickerRequestSourceWorktree  TargetPickerRequestSource = "workspace_new_worktree"
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
	FeishuTargetPickerSourceGitWorktree    FeishuTargetPickerSourceKind = "git_worktree"
)

type FeishuTargetPickerPage string

const (
	FeishuTargetPickerPageMode           FeishuTargetPickerPage = "mode"
	FeishuTargetPickerPageTarget         FeishuTargetPickerPage = "target"
	FeishuTargetPickerPageSource         FeishuTargetPickerPage = "source"
	FeishuTargetPickerPageLocalDirectory FeishuTargetPickerPage = "local_directory"
	FeishuTargetPickerPageGit            FeishuTargetPickerPage = "git"
	FeishuTargetPickerPageWorktree       FeishuTargetPickerPage = "worktree"
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
	FeishuTargetPickerPathFieldLocalDirectory    = "local_directory"
	FeishuTargetPickerPathFieldGitParentDir      = "git_parent_dir"
	FeishuTargetPickerGitRepoURLFieldName        = "target_picker_git_repo_url"
	FeishuTargetPickerGitDirectoryNameFieldName  = "target_picker_git_directory_name"
	FeishuTargetPickerWorktreeBranchFieldName    = "target_picker_worktree_branch_name"
	FeishuTargetPickerWorktreeDirectoryFieldName = "target_picker_worktree_directory_name"
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
	PickerID                 string
	MessageID                string
	Title                    string
	Source                   TargetPickerRequestSource
	CatalogFamilyID          string
	CatalogVariantID         string
	CatalogBackend           agentproto.Backend
	Stage                    FeishuTargetPickerStage
	Page                     FeishuTargetPickerPage
	StageLabel               string
	Question                 string
	BodySections             []FeishuCardTextSection
	NoticeSections           []FeishuCardTextSection
	Phase                    frontstagecontract.Phase
	ActionPolicy             frontstagecontract.ActionPolicy
	Sealed                   bool
	StatusTitle              string
	StatusText               string
	StatusSections           []FeishuCardTextSection
	StatusFooter             string
	CanCancelProcessing      bool
	ProcessingCancelLabel    string
	CanGoBack                bool
	BackLabel                string
	BackCommandText          string
	SelectedMode             FeishuTargetPickerMode
	SelectedSource           FeishuTargetPickerSourceKind
	ShowModeSwitch           bool
	ShowWorkspaceSelect      bool
	ShowSessionSelect        bool
	ShowSourceSelect         bool
	WorkspaceSelectionLocked bool
	LockedWorkspaceKey       string
	AllowNewThread           bool
	ModePlaceholder          string
	WorkspacePlaceholder     string
	SessionPlaceholder       string
	SourcePlaceholder        string
	WorkspaceCursor          int
	SessionCursor            int
	SelectedWorkspaceKey     string
	SelectedSessionValue     string
	SelectedWorkspaceLabel   string
	SelectedWorkspaceMeta    string
	SelectedSessionLabel     string
	SelectedSessionMeta      string
	ConfirmLabel             string
	// ConfirmValidatesOnSubmit keeps the confirm button clickable when the
	// current page depends on Feishu form inputs that cannot be live-validated.
	ConfirmValidatesOnSubmit bool
	CanConfirm               bool
	Hint                     string
	ModeOptions              []FeishuTargetPickerModeOption
	WorkspaceOptions         []FeishuTargetPickerWorkspaceOption
	SessionOptions           []FeishuTargetPickerSessionOption
	SourceOptions            []FeishuTargetPickerSourceOption
	AddModeSummary           string
	AddModeDetail            string
	SourceUnavailableHint    string
	LocalDirectoryPath       string
	GitParentDir             string
	GitRepoURL               string
	GitDirectoryName         string
	GitFinalPath             string
	WorktreeBranchName       string
	WorktreeDirectoryName    string
	WorktreeFinalPath        string
	Messages                 []FeishuTargetPickerMessage
	SourceMessages           []FeishuTargetPickerMessage
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

func NormalizeFeishuTargetPickerView(view FeishuTargetPickerView) FeishuTargetPickerView {
	frame := frontstagecontract.NormalizeFrame(frontstagecontract.Frame{
		OwnerKind:    frontstagecontract.OwnerCardTargetPicker,
		Phase:        normalizeTargetPickerPhase(view),
		ActionPolicy: normalizeTargetPickerActionPolicy(view),
	})
	view.Phase = frame.Phase
	view.ActionPolicy = frame.ActionPolicy
	view.Sealed = frontstagecontract.SealedForPhase(frame.Phase)
	return view
}

func normalizeTargetPickerPhase(view FeishuTargetPickerView) frontstagecontract.Phase {
	if view.Phase != "" {
		return view.Phase
	}
	switch view.Stage {
	case FeishuTargetPickerStageProcessing:
		return frontstagecontract.PhaseProcessing
	case FeishuTargetPickerStageSucceeded:
		return frontstagecontract.PhaseSucceeded
	case FeishuTargetPickerStageFailed:
		return frontstagecontract.PhaseFailed
	case FeishuTargetPickerStageCancelled:
		return frontstagecontract.PhaseCancelled
	default:
		return frontstagecontract.PhaseEditing
	}
}

func normalizeTargetPickerActionPolicy(view FeishuTargetPickerView) frontstagecontract.ActionPolicy {
	if view.ActionPolicy != "" {
		return view.ActionPolicy
	}
	if normalizeTargetPickerPhase(view) == frontstagecontract.PhaseProcessing && view.CanCancelProcessing {
		return frontstagecontract.ActionPolicyCancelOnly
	}
	return ""
}
