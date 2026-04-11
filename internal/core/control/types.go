package control

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type ActionKind string

const (
	ActionListInstances              ActionKind = "surface.menu.list_instances"
	ActionStatus                     ActionKind = "surface.menu.status"
	ActionStop                       ActionKind = "surface.menu.stop"
	ActionNewThread                  ActionKind = "surface.menu.new_thread"
	ActionKillInstance               ActionKind = "surface.menu.kill_instance"
	ActionRemovedCommand             ActionKind = "surface.command.removed"
	ActionShowCommandHelp            ActionKind = "surface.command.help"
	ActionShowCommandMenu            ActionKind = "surface.command.menu"
	ActionDebugCommand               ActionKind = "surface.command.debug"
	ActionUpgradeCommand             ActionKind = "surface.command.upgrade"
	ActionStartCommandCapture        ActionKind = "surface.command.capture.start"
	ActionCancelCommandCapture       ActionKind = "surface.command.capture.cancel"
	ActionModelCommand               ActionKind = "surface.command.model"
	ActionReasoningCommand           ActionKind = "surface.command.reasoning"
	ActionAccessCommand              ActionKind = "surface.command.access"
	ActionAutoContinueCommand        ActionKind = "surface.command.auto_continue"
	ActionModeCommand                ActionKind = "surface.command.mode"
	ActionRespondRequest             ActionKind = "surface.request.respond"
	ActionTextMessage                ActionKind = "surface.message.text"
	ActionImageMessage               ActionKind = "surface.message.image"
	ActionReactionCreated            ActionKind = "surface.message.reaction.created"
	ActionMessageRecalled            ActionKind = "surface.message.recalled"
	ActionSelectPrompt               ActionKind = "surface.selection.prompt"
	ActionAttachInstance             ActionKind = "surface.button.attach_instance"
	ActionAttachWorkspace            ActionKind = "surface.button.attach_workspace"
	ActionShowAllWorkspaces          ActionKind = "surface.button.show_all_workspaces"
	ActionShowRecentWorkspaces       ActionKind = "surface.button.show_recent_workspaces"
	ActionShowAllThreadWorkspaces    ActionKind = "surface.button.show_all_thread_workspaces"
	ActionShowRecentThreadWorkspaces ActionKind = "surface.button.show_recent_thread_workspaces"
	ActionShowThreads                ActionKind = "surface.button.show_threads"
	ActionShowAllThreads             ActionKind = "surface.button.show_all_threads"
	ActionShowScopedThreads          ActionKind = "surface.button.show_scoped_threads"
	ActionShowWorkspaceThreads       ActionKind = "surface.button.show_workspace_threads"
	ActionUseThread                  ActionKind = "surface.button.use_thread"
	ActionConfirmKickThread          ActionKind = "surface.button.confirm_kick_thread"
	ActionCancelKickThread           ActionKind = "surface.button.cancel_kick_thread"
	ActionFollowLocal                ActionKind = "surface.button.follow_local"
	ActionDetach                     ActionKind = "surface.button.detach"
	ActionVSCodeMigrate              ActionKind = "surface.button.vscode_migrate"
)

type InboundLifecycleVerdict string

const (
	InboundLifecycleCurrent InboundLifecycleVerdict = "current"
	InboundLifecycleOld     InboundLifecycleVerdict = "old"
	InboundLifecycleOldCard InboundLifecycleVerdict = "old_card"
)

type ActionInboundMeta struct {
	EventID               string
	EventType             string
	EventCreateTime       time.Time
	RequestID             string
	MessageCreateTime     time.Time
	MenuClickTime         time.Time
	OpenMessageID         string
	CardDaemonLifecycleID string
	LifecycleVerdict      InboundLifecycleVerdict
	LifecycleReason       string
}

type Action struct {
	Kind                ActionKind
	GatewayID           string
	SurfaceSessionID    string
	ChatID              string
	ActorUserID         string
	MessageID           string
	Text                string
	Inputs              []agentproto.Input
	PromptID            string
	OptionID            string
	RequestID           string
	RequestType         string
	RequestOptionID     string
	RequestAnswers      map[string][]string
	Approved            bool
	CommandID           string
	InstanceID          string
	WorkspaceKey        string
	ThreadID            string
	AllowCrossWorkspace bool
	LocalPath           string
	MIMEType            string
	ReactionType        string
	TargetMessageID     string
	Inbound             *ActionInboundMeta
}

type SelectionPromptKind string

const (
	SelectionPromptAttachInstance  SelectionPromptKind = "attach_instance"
	SelectionPromptAttachWorkspace SelectionPromptKind = "attach_workspace"
	SelectionPromptUseThread       SelectionPromptKind = "use_thread"
	SelectionPromptKickThread      SelectionPromptKind = "kick_thread"
)

type SelectionOption struct {
	Index               int
	OptionID            string
	Label               string
	Subtitle            string
	ButtonLabel         string
	GroupKey            string
	GroupLabel          string
	AgeText             string
	MetaText            string
	ActionKind          string
	IsCurrent           bool
	Disabled            bool
	AllowCrossWorkspace bool
}

// FeishuDirectSelectionPrompt is a retained direct card DTO for the remaining
// non-controller Feishu prompt paths.
type FeishuDirectSelectionPrompt struct {
	PromptID     string
	Kind         SelectionPromptKind
	Layout       string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Title        string
	Hint         string
	ContextTitle string
	ContextText  string
	ContextKey   string
	Options      []SelectionOption
}

type Snapshot struct {
	SurfaceSessionID string
	ActorUserID      string
	ProductMode      string
	WorkspaceKey     string
	Attachment       AttachmentSummary
	PendingHeadless  PendingHeadlessSummary
	NextPrompt       PromptRouteSummary
	Gate             GateSummary
	Dispatch         DispatchSummary
	AutoContinue     AutoContinueSummary
	Instances        []InstanceSummary
	Threads          []ThreadSummary
}

type AttachmentSummary struct {
	InstanceID            string
	ObjectType            string
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

type AutoContinueSummary struct {
	Enabled             bool
	PendingReason       string
	PendingDueAt        time.Time
	ConsecutiveCount    int
	LastTriggeredTurnID string
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
	ThumbsUp        bool
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

type RequestPromptQuestionOption struct {
	Label       string
	Description string
}

type RequestPromptQuestion struct {
	ID             string
	Header         string
	Question       string
	AllowOther     bool
	Secret         bool
	Options        []RequestPromptQuestionOption
	Placeholder    string
	DefaultValue   string
	DirectResponse bool
}

// FeishuDirectRequestPrompt is a retained direct card DTO for request cards
// that still cross the boundary without a separate Feishu view model.
type FeishuDirectRequestPrompt struct {
	RequestID   string
	RequestType string
	Title       string
	Body        string
	ThreadID    string
	ThreadTitle string
	Options     []RequestPromptOption
	Questions   []RequestPromptQuestion
}

type CommandCatalogButtonKind string

const (
	CommandCatalogButtonRunCommand           CommandCatalogButtonKind = "run_command"
	CommandCatalogButtonStartCommandCapture  CommandCatalogButtonKind = "start_command_capture"
	CommandCatalogButtonCancelCommandCapture CommandCatalogButtonKind = "cancel_command_capture"
)

type CommandCatalogFormFieldKind string

const (
	CommandCatalogFormFieldText CommandCatalogFormFieldKind = "text"
)

type CommandCatalogDisplayStyle string

const (
	CommandCatalogDisplayDefault        CommandCatalogDisplayStyle = "default"
	CommandCatalogDisplayCompactButtons CommandCatalogDisplayStyle = "compact_buttons"
)

type CommandCatalogBreadcrumb struct {
	Label string
}

type CommandCatalogButton struct {
	Label       string
	Kind        CommandCatalogButtonKind
	CommandText string
	CommandID   string
	Style       string
	Disabled    bool
}

type CommandCatalogFormField struct {
	Name         string
	Kind         CommandCatalogFormFieldKind
	Label        string
	Placeholder  string
	DefaultValue string
}

type CommandCatalogForm struct {
	CommandID   string
	CommandText string
	SubmitLabel string
	Field       CommandCatalogFormField
}

type CommandCatalogEntry struct {
	Title       string
	Commands    []string
	Description string
	Examples    []string
	Buttons     []CommandCatalogButton
	Form        *CommandCatalogForm
}

type CommandCatalogSection struct {
	Title   string
	Entries []CommandCatalogEntry
}

// FeishuDirectCommandCatalog is a retained direct card DTO for static help and
// daemon-owned command cards that are intentionally not routed through the
// newer command view path.
type FeishuDirectCommandCatalog struct {
	Title          string
	Summary        string
	Interactive    bool
	DisplayStyle   CommandCatalogDisplayStyle
	Breadcrumbs    []CommandCatalogBreadcrumb
	Sections       []CommandCatalogSection
	RelatedButtons []CommandCatalogButton
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

type FinalTurnUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type FinalTurnSummary struct {
	Elapsed   time.Duration
	ThreadCWD string
	Usage     *FinalTurnUsage
}

type ImageOutput struct {
	ThreadID    string
	TurnID      string
	ItemID      string
	Prompt      string
	SavedPath   string
	ImageBase64 string
}

type UIEventKind string

const (
	UIEventSnapshot                    UIEventKind = "snapshot.updated"
	UIEventFeishuDirectSelectionPrompt UIEventKind = "selection.prompt"
	UIEventFeishuDirectCommandCatalog  UIEventKind = "command.catalog"
	UIEventFeishuDirectRequestPrompt   UIEventKind = "request.prompt"
	UIEventPendingInput                UIEventKind = "pending.input.state"
	UIEventNotice                      UIEventKind = "notice"
	UIEventThreadSelectionChange       UIEventKind = "thread.selection.changed"
	UIEventBlockCommitted              UIEventKind = "block.committed"
	UIEventImageOutput                 UIEventKind = "image.output"
	UIEventAgentCommand                UIEventKind = "agent.command"
	UIEventDaemonCommand               UIEventKind = "daemon.command"
)

type DaemonCommandKind string

const (
	DaemonCommandStartHeadless DaemonCommandKind = "headless.start"
	DaemonCommandKillHeadless  DaemonCommandKind = "headless.kill"
	DaemonCommandDebug         DaemonCommandKind = "debug.command"
	DaemonCommandUpgrade       DaemonCommandKind = "upgrade.command"
	DaemonCommandVSCodeMigrate DaemonCommandKind = "vscode.migrate"
)

type DaemonCommand struct {
	Kind             DaemonCommandKind
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
	InstanceID       string
	ThreadID         string
	ThreadTitle      string
	ThreadCWD        string
	AutoRestore      bool
	Text             string
}

type UIEvent struct {
	Kind                        UIEventKind
	GatewayID                   string
	SurfaceSessionID            string
	DaemonLifecycleID           string
	SourceMessageID             string
	SourceMessagePreview        string
	InlineReplaceCurrentCard    bool
	Snapshot                    *Snapshot
	FeishuDirectSelectionPrompt *FeishuDirectSelectionPrompt
	FeishuSelectionView         *FeishuSelectionView
	FeishuSelectionContext      *FeishuUISelectionContext
	FeishuDirectCommandCatalog  *FeishuDirectCommandCatalog
	FeishuCommandView           *FeishuCommandView
	FeishuCommandContext        *FeishuUICommandContext
	FeishuDirectRequestPrompt   *FeishuDirectRequestPrompt
	FeishuRequestContext        *FeishuUIRequestContext
	PendingInput                *PendingInputState
	Notice                      *Notice
	ThreadSelection             *ThreadSelectionChanged
	Block                       *render.Block
	ImageOutput                 *ImageOutput
	FileChangeSummary           *FileChangeSummary
	FinalTurnSummary            *FinalTurnSummary
	Command                     *agentproto.Command
	DaemonCommand               *DaemonCommand
}
