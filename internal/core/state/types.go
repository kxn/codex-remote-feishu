package state

import (
	"time"

	"fschannel/internal/core/agentproto"
)

type RouteMode string

const (
	RouteModePinned      RouteMode = "pinned"
	RouteModeFollowLocal RouteMode = "follow_local"
	RouteModeUnbound     RouteMode = "unbound"
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
}

type InstanceRecord struct {
	InstanceID              string
	DisplayName             string
	WorkspaceRoot           string
	WorkspaceKey            string
	ShortName               string
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
}

type SurfaceConsoleRecord struct {
	SurfaceSessionID   string
	Platform           string
	ChatID             string
	ActorUserID        string
	AttachedInstanceID string
	SelectedThreadID   string
	RouteMode          RouteMode
	DispatchMode       DispatchMode
	ActiveTurnOrigin   agentproto.InitiatorKind
	ActiveQueueItemID  string
	QueuedQueueItemIDs []string
	StagedImages       map[string]*StagedImageRecord
	QueueItems         map[string]*QueueItemRecord
	PromptOverride     ModelConfigRecord
	SelectionPrompt    *SelectionPromptRecord
	LastSelection      *SelectionAnnouncementRecord
}

type SelectionAnnouncementRecord struct {
	ThreadID  string
	RouteMode string
	Title     string
	Preview   string
}

type SelectionPromptRecord struct {
	PromptID  string
	Kind      string
	CreatedAt time.Time
	ExpiresAt time.Time
	Options   []SelectionOptionRecord
}

type SelectionOptionRecord struct {
	Index    int
	OptionID string
	Label    string
	Subtitle string
	Current  bool
	Disabled bool
}

type QueueItemRecord struct {
	ID                 string
	SurfaceSessionID   string
	SourceMessageID    string
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
