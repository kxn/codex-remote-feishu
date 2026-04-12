package agentproto

import "time"

type InitiatorKind string

const (
	InitiatorUnknown        InitiatorKind = "unknown"
	InitiatorLocalUI        InitiatorKind = "local_ui"
	InitiatorInternalHelper InitiatorKind = "internal_helper"
	InitiatorRemoteSurface  InitiatorKind = "remote_surface"
)

type TrafficClass string

const (
	TrafficClassPrimary        TrafficClass = "primary"
	TrafficClassInternalHelper TrafficClass = "internal_helper"
)

type Initiator struct {
	Kind             InitiatorKind `json:"kind"`
	SurfaceSessionID string        `json:"surfaceSessionId,omitempty"`
}

type EventKind string

const (
	EventThreadsSnapshot          EventKind = "threads.snapshot"
	EventThreadDiscovered         EventKind = "thread.discovered"
	EventThreadFocused            EventKind = "thread.focused"
	EventConfigObserved           EventKind = "config.observed"
	EventLocalInteractionObserved EventKind = "local.interaction.observed"
	EventThreadTokenUsageUpdated  EventKind = "thread.token_usage.updated"
	EventTurnPlanUpdated          EventKind = "turn.plan.updated"
	EventTurnStarted              EventKind = "turn.started"
	EventTurnCompleted            EventKind = "turn.completed"
	EventItemStarted              EventKind = "item.started"
	EventItemDelta                EventKind = "item.delta"
	EventItemCompleted            EventKind = "item.completed"
	EventRequestStarted           EventKind = "request.started"
	EventRequestResolved          EventKind = "request.resolved"
	EventSystemError              EventKind = "system.error"
)

type FileChangeKind string

const (
	FileChangeAdd    FileChangeKind = "add"
	FileChangeDelete FileChangeKind = "delete"
	FileChangeUpdate FileChangeKind = "update"
)

type FileChangeRecord struct {
	Path     string         `json:"path,omitempty"`
	Kind     FileChangeKind `json:"kind,omitempty"`
	MovePath string         `json:"movePath,omitempty"`
	Diff     string         `json:"diff,omitempty"`
}

type Event struct {
	Seq             uint64                 `json:"seq,omitempty"`
	Kind            EventKind              `json:"kind"`
	ThreadID        string                 `json:"threadId,omitempty"`
	TurnID          string                 `json:"turnId,omitempty"`
	ItemID          string                 `json:"itemId,omitempty"`
	RequestID       string                 `json:"requestId,omitempty"`
	Status          string                 `json:"status,omitempty"`
	ErrorMessage    string                 `json:"errorMessage,omitempty"`
	CWD             string                 `json:"cwd,omitempty"`
	FocusSource     string                 `json:"focusSource,omitempty"`
	Action          string                 `json:"action,omitempty"`
	ItemKind        string                 `json:"itemKind,omitempty"`
	Delta           string                 `json:"delta,omitempty"`
	Name            string                 `json:"name,omitempty"`
	Preview         string                 `json:"preview,omitempty"`
	Model           string                 `json:"model,omitempty"`
	ReasoningEffort string                 `json:"reasoningEffort,omitempty"`
	AccessMode      string                 `json:"accessMode,omitempty"`
	ConfigScope     string                 `json:"configScope,omitempty"`
	Loaded          bool                   `json:"loaded,omitempty"`
	Archived        bool                   `json:"archived,omitempty"`
	TrafficClass    TrafficClass           `json:"trafficClass,omitempty"`
	Initiator       Initiator              `json:"initiator,omitempty"`
	Problem         *ErrorInfo             `json:"problem,omitempty"`
	TokenUsage      *ThreadTokenUsage      `json:"tokenUsage,omitempty"`
	PlanSnapshot    *TurnPlanSnapshot      `json:"planSnapshot,omitempty"`
	Metadata        map[string]any         `json:"metadata,omitempty"`
	Threads         []ThreadSnapshotRecord `json:"threads,omitempty"`
	FileChanges     []FileChangeRecord     `json:"fileChanges,omitempty"`
}

type ThreadSnapshotRecord struct {
	ThreadID        string `json:"threadId"`
	Name            string `json:"name,omitempty"`
	Preview         string `json:"preview,omitempty"`
	CWD             string `json:"cwd,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Loaded          bool   `json:"loaded"`
	Archived        bool   `json:"archived"`
	State           string `json:"state,omitempty"`
	ListOrder       int    `json:"listOrder,omitempty"`
}

type CommandKind string

const (
	CommandPromptSend     CommandKind = "prompt.send"
	CommandTurnSteer      CommandKind = "turn.steer"
	CommandTurnInterrupt  CommandKind = "turn.interrupt"
	CommandRequestRespond CommandKind = "request.respond"
	CommandThreadsRefresh CommandKind = "threads.refresh"
	CommandProcessExit    CommandKind = "process.exit"
)

type InputKind string

const (
	InputText        InputKind = "text"
	InputLocalImage  InputKind = "local_image"
	InputRemoteImage InputKind = "remote_image"
)

type Input struct {
	Type     InputKind `json:"type"`
	Text     string    `json:"text,omitempty"`
	Path     string    `json:"path,omitempty"`
	URL      string    `json:"url,omitempty"`
	MIMEType string    `json:"mimeType,omitempty"`
}

type Command struct {
	CommandID string          `json:"commandId,omitempty"`
	IssuedAt  time.Time       `json:"issuedAt,omitempty"`
	Kind      CommandKind     `json:"kind"`
	Origin    Origin          `json:"origin"`
	Target    Target          `json:"target"`
	Prompt    Prompt          `json:"prompt,omitempty"`
	Overrides PromptOverrides `json:"overrides,omitempty"`
	Request   Request         `json:"request,omitempty"`
}

type Origin struct {
	Surface   string `json:"surface,omitempty"`
	UserID    string `json:"userId,omitempty"`
	ChatID    string `json:"chatId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
}

type Target struct {
	ThreadID               string `json:"threadId,omitempty"`
	CreateThreadIfMissing  bool   `json:"createThreadIfMissing,omitempty"`
	CWD                    string `json:"cwd,omitempty"`
	TurnID                 string `json:"turnId,omitempty"`
	UseActiveTurnIfOmitted bool   `json:"useActiveTurnIfOmitted,omitempty"`
}

type Prompt struct {
	Inputs []Input `json:"inputs,omitempty"`
}

type PromptOverrides struct {
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	AccessMode      string `json:"accessMode,omitempty"`
}

type Request struct {
	RequestID string         `json:"requestId,omitempty"`
	Response  map[string]any `json:"response,omitempty"`
}
