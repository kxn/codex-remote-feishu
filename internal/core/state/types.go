package state

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type RouteMode string

const (
	RouteModePinned         RouteMode = "pinned"
	RouteModeFollowLocal    RouteMode = "follow_local"
	RouteModeNewThreadReady RouteMode = "new_thread_ready"
	RouteModeUnbound        RouteMode = "unbound"
)

type DispatchMode string

const (
	DispatchModeNormal         DispatchMode = "normal"
	DispatchModeHandoffWait    DispatchMode = "handoff_wait"
	DispatchModePausedForLocal DispatchMode = "paused_for_local"
)

type ProductMode string

const (
	ProductModeNormal ProductMode = "normal"
	ProductModeVSCode ProductMode = "vscode"
)

func NormalizeProductMode(mode ProductMode) ProductMode {
	switch mode {
	case ProductModeVSCode:
		return ProductModeVSCode
	default:
		return ProductModeNormal
	}
}

type SurfaceVerbosity string

const (
	SurfaceVerbosityQuiet   SurfaceVerbosity = "quiet"
	SurfaceVerbosityNormal  SurfaceVerbosity = "normal"
	SurfaceVerbosityVerbose SurfaceVerbosity = "verbose"
)

func NormalizeSurfaceVerbosity(value SurfaceVerbosity) SurfaceVerbosity {
	switch value {
	case SurfaceVerbosityQuiet:
		return SurfaceVerbosityQuiet
	case SurfaceVerbosityVerbose:
		return SurfaceVerbosityVerbose
	default:
		return SurfaceVerbosityNormal
	}
}

type QueueItemStatus string

const (
	QueueItemQueued      QueueItemStatus = "queued"
	QueueItemDispatching QueueItemStatus = "dispatching"
	QueueItemRunning     QueueItemStatus = "running"
	QueueItemSteering    QueueItemStatus = "steering"
	QueueItemSteered     QueueItemStatus = "steered"
	QueueItemCompleted   QueueItemStatus = "completed"
	QueueItemFailed      QueueItemStatus = "failed"
	QueueItemDiscarded   QueueItemStatus = "discarded"
)

type QueueItemSourceKind string

const (
	QueueItemSourceUser         QueueItemSourceKind = "user"
	QueueItemSourceAutoContinue QueueItemSourceKind = "auto_continue"
)

type ImageState string

const (
	ImageStaged    ImageState = "staged"
	ImageCancelled ImageState = "cancelled"
	ImageBound     ImageState = "bound"
	ImageDiscarded ImageState = "discarded"
)

type AutoContinueReason string

const (
	AutoContinueReasonIncompleteStop   AutoContinueReason = "incomplete_stop"
	AutoContinueReasonRetryableFailure AutoContinueReason = "retryable_failure"
)

type Root struct {
	Instances         map[string]*InstanceRecord
	Surfaces          map[string]*SurfaceConsoleRecord
	WorkspaceDefaults map[string]ModelConfigRecord
}

type ModelConfigRecord struct {
	Model           string
	ReasoningEffort string
	AccessMode      string
}

type InstanceRecord struct {
	InstanceID              string
	DisplayName             string
	WorkspaceRoot           string
	WorkspaceKey            string
	ShortName               string
	Source                  string
	Managed                 bool
	PID                     int
	Online                  bool
	ObservedFocusedThreadID string
	ActiveThreadID          string
	ActiveTurnID            string
	CWDDefaults             map[string]ModelConfigRecord
	Threads                 map[string]*ThreadRecord
}

type ThreadRecord struct {
	ThreadID                string
	Name                    string
	Preview                 string
	FirstUserMessage        string
	LastUserMessage         string
	LastAssistantMessage    string
	CWD                     string
	State                   string
	ExplicitModel           string
	ExplicitReasoningEffort string
	Loaded                  bool
	Archived                bool
	TrafficClass            agentproto.TrafficClass
	TokenUsage              *agentproto.ThreadTokenUsage
	UndeliveredReplay       *ThreadReplayRecord
	LastUsedAt              time.Time
	ListOrder               int
}

type ThreadReplayKind string

const (
	ThreadReplayAssistantFinal ThreadReplayKind = "assistant_final"
	ThreadReplayNotice         ThreadReplayKind = "notice"
)

type ThreadReplayRecord struct {
	Kind           ThreadReplayKind
	TurnID         string
	ItemID         string
	Text           string
	NoticeCode     string
	NoticeTitle    string
	NoticeText     string
	NoticeThemeKey string
}

type SurfaceConsoleRecord struct {
	SurfaceSessionID     string
	Platform             string
	GatewayID            string
	ChatID               string
	ActorUserID          string
	ProductMode          ProductMode
	Verbosity            SurfaceVerbosity
	ClaimedWorkspaceKey  string
	AttachedInstanceID   string
	SelectedThreadID     string
	LastInboundAt        time.Time
	RouteMode            RouteMode
	Abandoning           bool
	DispatchMode         DispatchMode
	ActiveTurnOrigin     agentproto.InitiatorKind
	ActiveQueueItemID    string
	QueuedQueueItemIDs   []string
	StagedImages         map[string]*StagedImageRecord
	QueueItems           map[string]*QueueItemRecord
	PreparedThreadCWD    string
	PreparedFromThreadID string
	PreparedAt           time.Time
	PromptOverride       ModelConfigRecord
	PendingHeadless      *HeadlessLaunchRecord
	PendingRequests      map[string]*RequestPromptRecord
	ActiveRequestCapture *RequestCaptureRecord
	ActiveCommandCapture *CommandCaptureRecord
	ActivePathPicker     *ActivePathPickerRecord
	ActiveExecProgress   *ExecCommandProgressRecord
	LastThreadHistory    *agentproto.ThreadHistoryRecord
	LastSelection        *SelectionAnnouncementRecord
	AutoContinue         AutoContinueRuntimeRecord
}

type ExecCommandProgressRecord struct {
	InstanceID    string
	ThreadID      string
	TurnID        string
	ItemID        string
	MessageID     string
	Commands      []string
	Command       string
	CWD           string
	Status        string
	LastEmittedAt time.Time
}

type AutoContinueRuntimeRecord struct {
	Enabled                      bool
	PendingReason                AutoContinueReason
	PendingDueAt                 time.Time
	ConsecutiveCount             int
	LastTriggeredTurnID          string
	PendingReplyToMessageID      string
	PendingReplyToMessagePreview string
	IncompleteStopCount          int
	RetryableFailureCount        int
	SuppressOnce                 bool
}

type HeadlessLaunchStatus string

const (
	HeadlessLaunchStarting HeadlessLaunchStatus = "starting"
)

type HeadlessLaunchPurpose string

const (
	HeadlessLaunchPurposeLegacy         HeadlessLaunchPurpose = ""
	HeadlessLaunchPurposeThreadRestore  HeadlessLaunchPurpose = "thread_restore"
	HeadlessLaunchPurposeFreshWorkspace HeadlessLaunchPurpose = "fresh_workspace"
)

type HeadlessLaunchRecord struct {
	InstanceID       string
	ThreadID         string
	ThreadTitle      string
	ThreadCWD        string
	ThreadName       string
	ThreadPreview    string
	RequestedAt      time.Time
	ExpiresAt        time.Time
	Status           HeadlessLaunchStatus
	Purpose          HeadlessLaunchPurpose
	PID              int
	SourceInstanceID string
	AutoRestore      bool
}

type SelectionAnnouncementRecord struct {
	ThreadID  string
	RouteMode string
	Title     string
	Preview   string
}

type RequestPromptOptionRecord struct {
	OptionID string
	Label    string
	Style    string
}

type RequestPromptQuestionOptionRecord struct {
	Label       string
	Description string
}

type RequestPromptQuestionRecord struct {
	ID             string
	Header         string
	Question       string
	AllowOther     bool
	Secret         bool
	Options        []RequestPromptQuestionOptionRecord
	Placeholder    string
	DefaultValue   string
	DirectResponse bool
}

type RequestPromptRecord struct {
	RequestID                string
	RequestType              string
	InstanceID               string
	ThreadID                 string
	TurnID                   string
	ItemID                   string
	Title                    string
	Body                     string
	Options                  []RequestPromptOptionRecord
	Questions                []RequestPromptQuestionRecord
	DraftAnswers             map[string]string
	CardRevision             int
	PendingDispatchCommandID string
	// SubmitWithUnansweredConfirmPending marks request_user_input cards that are
	// waiting for explicit user confirmation before submitting unanswered fields.
	SubmitWithUnansweredConfirmPending bool
	SubmitWithUnansweredMissingLabels  []string
	CreatedAt                          time.Time
}

type RequestCaptureRecord struct {
	RequestID   string
	RequestType string
	InstanceID  string
	ThreadID    string
	TurnID      string
	Mode        string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type CommandCaptureRecord struct {
	CommandID string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type QueueItemRecord struct {
	ID                    string
	SurfaceSessionID      string
	SourceKind            QueueItemSourceKind
	SourceMessageID       string
	SourceMessagePreview  string
	SourceMessageIDs      []string
	ReplyToMessageID      string
	ReplyToMessagePreview string
	Inputs                []agentproto.Input
	FrozenThreadID        string
	FrozenCWD             string
	FrozenOverride        ModelConfigRecord
	RouteModeAtEnqueue    RouteMode
	Status                QueueItemStatus
}

type StagedImageRecord struct {
	ImageID          string
	SurfaceSessionID string
	SourceMessageID  string
	LocalPath        string
	MIMEType         string
	State            ImageState
}

func NewRoot() *Root {
	return &Root{
		Instances:         map[string]*InstanceRecord{},
		Surfaces:          map[string]*SurfaceConsoleRecord{},
		WorkspaceDefaults: map[string]ModelConfigRecord{},
	}
}
