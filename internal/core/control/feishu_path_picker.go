package control

import "time"

type PathPickerMode string

const (
	PathPickerModeDirectory PathPickerMode = "directory"
	PathPickerModeFile      PathPickerMode = "file"
)

type PathPickerEntryKind string

const (
	PathPickerEntryDirectory PathPickerEntryKind = "directory"
	PathPickerEntryFile      PathPickerEntryKind = "file"
)

type PathPickerEntryActionKind string

const (
	PathPickerEntryActionNone   PathPickerEntryActionKind = ""
	PathPickerEntryActionEnter  PathPickerEntryActionKind = "enter"
	PathPickerEntryActionSelect PathPickerEntryActionKind = "select"
)

type PathPickerRequest struct {
	Mode         PathPickerMode
	Title        string
	RootPath     string
	InitialPath  string
	Hint         string
	ConfirmLabel string
	CancelLabel  string
	ExpireAfter  time.Duration
	OwnerFlowID  string
	ConsumerKind string
	ConsumerMeta map[string]string
}

type PathPickerResult struct {
	PickerID     string
	Mode         PathPickerMode
	RootPath     string
	CurrentPath  string
	SelectedPath string
	OwnerUserID  string
	ConsumerKind string
	ConsumerMeta map[string]string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// FeishuPathPickerView is the UI-owned read model for the reusable Feishu
// file/directory picker.
type FeishuPathPickerView struct {
	PickerID       string
	MessageID      string
	Mode           PathPickerMode
	Title          string
	RootPath       string
	CurrentPath    string
	SelectedPath   string
	ConfirmLabel   string
	CancelLabel    string
	CanGoUp        bool
	CanConfirm     bool
	Hint           string
	Terminal       bool
	StatusTitle    string
	StatusText     string
	StatusSections []FeishuCardTextSection
	StatusFooter   string
	Entries        []FeishuPathPickerEntry
}

type FeishuPathPickerEntry struct {
	Name           string
	Label          string
	Kind           PathPickerEntryKind
	ActionKind     PathPickerEntryActionKind
	Disabled       bool
	DisabledReason string
	Selected       bool
}
