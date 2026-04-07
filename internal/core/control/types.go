package control

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type ActionKind string

const (
	ActionListInstances     ActionKind = "surface.menu.list_instances"
	ActionStatus            ActionKind = "surface.menu.status"
	ActionStop              ActionKind = "surface.menu.stop"
	ActionNewThread         ActionKind = "surface.menu.new_thread"
	ActionNewInstance       ActionKind = "surface.menu.new_instance"
	ActionKillInstance      ActionKind = "surface.menu.kill_instance"
	ActionModelCommand      ActionKind = "surface.command.model"
	ActionReasoningCommand  ActionKind = "surface.command.reasoning"
	ActionAccessCommand     ActionKind = "surface.command.access"
	ActionRespondRequest    ActionKind = "surface.request.respond"
	ActionTextMessage       ActionKind = "surface.message.text"
	ActionImageMessage      ActionKind = "surface.message.image"
	ActionReactionCreated   ActionKind = "surface.message.reaction.created"
	ActionMessageRecalled   ActionKind = "surface.message.recalled"
	ActionSelectPrompt      ActionKind = "surface.selection.prompt"
	ActionAttachInstance    ActionKind = "surface.button.attach_instance"
	ActionShowThreads       ActionKind = "surface.button.show_threads"
	ActionShowAllThreads    ActionKind = "surface.button.show_all_threads"
	ActionUseThread         ActionKind = "surface.button.use_thread"
	ActionResumeHeadless    ActionKind = "surface.button.resume_headless_thread"
	ActionConfirmKickThread ActionKind = "surface.button.confirm_kick_thread"
	ActionCancelKickThread  ActionKind = "surface.button.cancel_kick_thread"
	ActionFollowLocal       ActionKind = "surface.button.follow_local"
	ActionDetach            ActionKind = "surface.button.detach"
)

type Action struct {
	Kind             ActionKind
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	MessageID        string
	Text             string
	Inputs           []agentproto.Input
	PromptID         string
	OptionID         string
	RequestID        string
	RequestType      string
	RequestOptionID  string
	Approved         bool
	InstanceID       string
	ThreadID         string
	LocalPath        string
	MIMEType         string
	TargetMessageID  string
}

type SelectionPromptKind string

const (
	SelectionPromptAttachInstance SelectionPromptKind = "attach_instance"
	SelectionPromptUseThread      SelectionPromptKind = "use_thread"
	SelectionPromptNewInstance    SelectionPromptKind = "new_instance_thread"
	SelectionPromptKickThread     SelectionPromptKind = "kick_thread"
)

type SelectionOption struct {
	Index       int
	OptionID    string
	Label       string
	Subtitle    string
	ButtonLabel string
	IsCurrent   bool
	Disabled    bool
}

type SelectionPrompt struct {
	PromptID  string
	Kind      SelectionPromptKind
	CreatedAt time.Time
	ExpiresAt time.Time
	Title     string
	Hint      string
	Options   []SelectionOption
}

type Snapshot struct {
	SurfaceSessionID string
	ActorUserID      string
	Attachment       AttachmentSummary
	PendingHeadless  PendingHeadlessSummary
	NextPrompt       PromptRouteSummary
	Gate             GateSummary
	Dispatch         DispatchSummary
	Instances        []InstanceSummary
	Threads          []ThreadSummary
}

type AttachmentSummary struct {
	InstanceID            string
	DisplayName           string
	Source                string
	Managed               bool
	PID                   int
	SelectedThreadID      string
	SelectedThreadTitle   string
	SelectedThreadPreview string
	RouteMode             string
	Abandoning            bool
}

type PendingHeadlessSummary struct {
	InstanceID  string
	ThreadID    string
	ThreadTitle string
	ThreadCWD   string
	Status      string
	PID         int
	ExpiresAt   time.Time
	RequestedAt time.Time
}

type PromptRouteSummary struct {
	RouteMode                      string
	ThreadID                       string
	ThreadTitle                    string
	CWD                            string
	CreateThread                   bool
	BaseModel                      string
	BaseReasoningEffort            string
	BaseModelSource                string
	BaseReasoningEffortSource      string
	OverrideModel                  string
	OverrideReasoningEffort        string
	OverrideAccessMode             string
	EffectiveModel                 string
	EffectiveReasoningEffort       string
	EffectiveAccessMode            string
	EffectiveModelSource           string
	EffectiveReasoningEffortSource string
	EffectiveAccessModeSource      string
}

type GateSummary struct {
	Kind                string
	PendingRequestCount int
}

type DispatchSummary struct {
	InstanceOnline   bool
	DispatchMode     string
	ActiveItemStatus string
	QueuedCount      int
}

type InstanceSummary struct {
	InstanceID              string
	DisplayName             string
	WorkspaceRoot           string
	WorkspaceKey            string
	Source                  string
	Managed                 bool
	PID                     int
	Online                  bool
	State                   string
	ObservedFocusedThreadID string
}

type ThreadSummary struct {
	ThreadID          string
	Name              string
	DisplayTitle      string
	Preview           string
	CWD               string
	State             string
	Model             string
	ReasoningEffort   string
	Loaded            bool
	IsObservedFocused bool
	IsSelected        bool
}

type PendingInputState struct {
	QueueItemID     string
	SourceMessageID string
	Status          string
	QueuePosition   int
	QueueOn         bool
	QueueOff        bool
	TypingOn        bool
	TypingOff       bool
	ThumbsDown      bool
}

type Notice struct {
	Code     string
	Title    string
	Text     string
	ThemeKey string
}

type ThreadSelectionChanged struct {
	ThreadID  string
	RouteMode string
	Title     string
	Preview   string
}

type RequestPromptOption struct {
	OptionID string
	Label    string
	Style    string
}

type RequestPrompt struct {
	RequestID   string
	RequestType string
	Title       string
	Body        string
	ThreadID    string
	ThreadTitle string
	Options     []RequestPromptOption
}

type FileChangeSummaryEntry struct {
	Path         string
	MovePath     string
	AddedLines   int
	RemovedLines int
}

type FileChangeSummary struct {
	ThreadID     string
	ThreadTitle  string
	FileCount    int
	AddedLines   int
	RemovedLines int
	Files        []FileChangeSummaryEntry
}

type UIEventKind string

const (
	UIEventSnapshot              UIEventKind = "snapshot.updated"
	UIEventSelectionPrompt       UIEventKind = "selection.prompt"
	UIEventRequestPrompt         UIEventKind = "request.prompt"
	UIEventPendingInput          UIEventKind = "pending.input.state"
	UIEventNotice                UIEventKind = "notice"
	UIEventThreadSelectionChange UIEventKind = "thread.selection.changed"
	UIEventBlockCommitted        UIEventKind = "block.committed"
	UIEventAgentCommand          UIEventKind = "agent.command"
	UIEventDaemonCommand         UIEventKind = "daemon.command"
)

type DaemonCommandKind string

const (
	DaemonCommandStartHeadless DaemonCommandKind = "headless.start"
	DaemonCommandKillHeadless  DaemonCommandKind = "headless.kill"
)

type DaemonCommand struct {
	Kind             DaemonCommandKind
	GatewayID        string
	SurfaceSessionID string
	InstanceID       string
	ThreadID         string
	ThreadTitle      string
	ThreadCWD        string
}

type UIEvent struct {
	Kind              UIEventKind
	GatewayID         string
	SurfaceSessionID  string
	Snapshot          *Snapshot
	SelectionPrompt   *SelectionPrompt
	RequestPrompt     *RequestPrompt
	PendingInput      *PendingInputState
	Notice            *Notice
	ThreadSelection   *ThreadSelectionChanged
	Block             *render.Block
	FileChangeSummary *FileChangeSummary
	Command           *agentproto.Command
	DaemonCommand     *DaemonCommand
}
