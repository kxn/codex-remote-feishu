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

// FeishuTargetPickerView is the UI-owned read model for the unified
// workspace/session target picker card.
type FeishuTargetPickerView struct {
	PickerID               string
	Title                  string
	Source                 TargetPickerRequestSource
	WorkspacePlaceholder   string
	SessionPlaceholder     string
	SelectedWorkspaceKey   string
	SelectedSessionValue   string
	SelectedWorkspaceLabel string
	SelectedWorkspaceMeta  string
	SelectedSessionLabel   string
	SelectedSessionMeta    string
	ConfirmLabel           string
	CanConfirm             bool
	Hint                   string
	WorkspaceOptions       []FeishuTargetPickerWorkspaceOption
	SessionOptions         []FeishuTargetPickerSessionOption
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

type TargetPickerResult struct {
	PickerID     string
	Source       TargetPickerRequestSource
	WorkspaceKey string
	SessionValue string
	OwnerUserID  string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}
