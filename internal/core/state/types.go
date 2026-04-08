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

type QueueItemStatus string

const (
	QueueItemQueued      QueueItemStatus = "queued"
	QueueItemDispatching QueueItemStatus = "dispatching"
	QueueItemRunning     QueueItemStatus = "running"
	QueueItemCompleted   QueueItemStatus = "completed"
	QueueItemFailed      QueueItemStatus = "failed"
	QueueItemDiscarded   QueueItemStatus = "discarded"
)

type ImageState string

const (
	ImageStaged    ImageState = "staged"
	ImageCancelled ImageState = "cancelled"
	ImageBound     ImageState = "bound"
	ImageDiscarded ImageState = "discarded"
)

type Root struct {
	Instances map[string]*InstanceRecord
	Surfaces  map[string]*SurfaceConsoleRecord
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
	CWD                     string
	State                   string
	ExplicitModel           string
	ExplicitReasoningEffort string
	Loaded                  bool
	Archived                bool
	TrafficClass            agentproto.TrafficClass
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
	AttachedInstanceID   string
	SelectedThreadID     string
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
	LastSelection        *SelectionAnnouncementRecord
}

type HeadlessLaunchStatus string

const (
	HeadlessLaunchStarting HeadlessLaunchStatus = "starting"
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
	PID              int
	SourceInstanceID string
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

type RequestPromptRecord struct {
	RequestID   string
	RequestType string
	InstanceID  string
	ThreadID    string
	TurnID      string
	Title       string
	Body        string
	Options     []RequestPromptOptionRecord
	CreatedAt   time.Time
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

type QueueItemRecord struct {
	ID                 string
	SurfaceSessionID   string
	SourceMessageID    string
	SourceMessageIDs   []string
	Inputs             []agentproto.Input
	FrozenThreadID     string
	FrozenCWD          string
	FrozenOverride     ModelConfigRecord
	RouteModeAtEnqueue RouteMode
	Status             QueueItemStatus
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
		Instances: map[string]*InstanceRecord{},
		Surfaces:  map[string]*SurfaceConsoleRecord{},
	}
}
