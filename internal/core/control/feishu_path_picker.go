package control

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

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
	Mode            PathPickerMode
	Title           string
	StageLabel      string
	Question        string
	RootPath        string
	InitialPath     string
	SourceMessageID string
	Hint            string
	ConfirmLabel    string
	CancelLabel     string
	ExpireAfter     time.Duration
	OwnerFlowID     string
	ConsumerKind    string
	ConsumerMeta    map[string]string
	EntryFilterKind string
	EntryFilterMeta map[string]string
}

type PathPickerResult struct {
	PickerID     string
	MessageID    string
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
	PickerID        string
	MessageID       string
	Mode            PathPickerMode
	Title           string
	StageLabel      string
	Question        string
	BodySections    []FeishuCardTextSection
	NoticeSections  []FeishuCardTextSection
	Phase           frontstagecontract.Phase
	ActionPolicy    frontstagecontract.ActionPolicy
	Sealed          bool
	RootPath        string
	CurrentPath     string
	SelectedPath    string
	DirectoryCursor int
	FileCursor      int
	ConfirmLabel    string
	CancelLabel     string
	CanGoUp         bool
	CanConfirm      bool
	Hint            string
	Terminal        bool
	StatusTitle     string
	StatusText      string
	StatusSections  []FeishuCardTextSection
	StatusFooter    string
	Entries         []FeishuPathPickerEntry
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

func NormalizeFeishuPathPickerView(view FeishuPathPickerView) FeishuPathPickerView {
	frame := frontstagecontract.NormalizeFrame(frontstagecontract.Frame{
		OwnerKind:    frontstagecontract.OwnerCardPathPicker,
		Phase:        normalizePathPickerPhase(view),
		ActionPolicy: view.ActionPolicy,
	})
	view.Phase = frame.Phase
	view.ActionPolicy = frame.ActionPolicy
	view.Sealed = frontstagecontract.SealedForPhase(frame.Phase)
	view.Terminal = frontstagecontract.IsTerminalPhase(frame.Phase)
	return view
}

func normalizePathPickerPhase(view FeishuPathPickerView) frontstagecontract.Phase {
	if view.Phase != "" {
		return view.Phase
	}
	if view.Terminal || view.Sealed {
		return frontstagecontract.PhaseSucceeded
	}
	return frontstagecontract.PhaseEditing
}
