package control

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type ActionKind string

const (
	ActionListInstances               ActionKind = "surface.menu.list_instances"
	ActionStatus                      ActionKind = "surface.menu.status"
	ActionStop                        ActionKind = "surface.menu.stop"
	ActionCompact                     ActionKind = "surface.menu.compact"
	ActionSteerAll                    ActionKind = "surface.menu.steer_all"
	ActionNewThread                   ActionKind = "surface.menu.new_thread"
	ActionAdminRoot                   ActionKind = "surface.command.admin"
	ActionAdminCommand                ActionKind = "surface.command.admin.subcommand"
	ActionWorkspaceRoot               ActionKind = "surface.command.workspace"
	ActionWorkspaceList               ActionKind = "surface.command.workspace.list"
	ActionWorkspaceNew                ActionKind = "surface.command.workspace.new"
	ActionWorkspaceNewDir             ActionKind = "surface.command.workspace.new_dir"
	ActionWorkspaceNewGit             ActionKind = "surface.command.workspace.new_git"
	ActionWorkspaceNewWorktree        ActionKind = "surface.command.workspace.new_worktree"
	ActionWorkspaceDetach             ActionKind = "surface.command.workspace.detach"
	ActionShowCommandHelp             ActionKind = "surface.command.help"
	ActionShowCommandMenu             ActionKind = "surface.command.menu"
	ActionShowHistory                 ActionKind = "surface.command.history"
	ActionReviewCommand               ActionKind = "surface.command.review"
	ActionDebugCommand                ActionKind = "surface.command.debug"
	ActionCronCommand                 ActionKind = "surface.command.cron"
	ActionUpgradeCommand              ActionKind = "surface.command.upgrade"
	ActionTurnPatchCommand            ActionKind = "surface.command.turn_patch"
	ActionTurnPatchRollback           ActionKind = "surface.command.turn_patch.rollback"
	ActionVSCodeMigrateCommand        ActionKind = "surface.command.vscode_migrate"
	ActionUpgradeOwnerFlow            ActionKind = "surface.command.upgrade_owner_flow"
	ActionModelCommand                ActionKind = "surface.command.model"
	ActionReasoningCommand            ActionKind = "surface.command.reasoning"
	ActionAccessCommand               ActionKind = "surface.command.access"
	ActionPlanCommand                 ActionKind = "surface.command.plan"
	ActionPlanProposalDecision        ActionKind = "surface.command.plan_proposal_decision"
	ActionVerboseCommand              ActionKind = "surface.command.verbose"
	ActionCodexProviderCommand        ActionKind = "surface.command.codex_provider"
	ActionAutoWhipCommand             ActionKind = "surface.command.auto_whip"
	ActionAutoContinueCommand         ActionKind = "surface.command.auto_continue"
	ActionModeCommand                 ActionKind = "surface.command.mode"
	ActionClaudeProfileCommand        ActionKind = "surface.command.claude_profile"
	ActionSendFile                    ActionKind = "surface.command.send_file"
	ActionRespondRequest              ActionKind = "surface.request.respond"
	ActionControlRequest              ActionKind = "surface.request.control"
	ActionTextMessage                 ActionKind = "surface.message.text"
	ActionImageMessage                ActionKind = "surface.message.image"
	ActionFileMessage                 ActionKind = "surface.message.file"
	ActionReactionCreated             ActionKind = "surface.message.reaction.created"
	ActionMessageRecalled             ActionKind = "surface.message.recalled"
	ActionReviewStart                 ActionKind = "surface.button.review_start"
	ActionReviewStartUncommitted      ActionKind = "surface.button.review_start_uncommitted"
	ActionReviewOpenCommitPicker      ActionKind = "surface.button.review_open_commit_picker"
	ActionReviewDiscard               ActionKind = "surface.button.review_discard"
	ActionReviewApply                 ActionKind = "surface.button.review_apply"
	ActionAttachInstance              ActionKind = "surface.button.attach_instance"
	ActionAttachWorkspace             ActionKind = "surface.button.attach_workspace"
	ActionShowAllWorkspaces           ActionKind = "surface.button.show_all_workspaces"
	ActionShowRecentWorkspaces        ActionKind = "surface.button.show_recent_workspaces"
	ActionShowAllThreadWorkspaces     ActionKind = "surface.button.show_all_thread_workspaces"
	ActionShowRecentThreadWorkspaces  ActionKind = "surface.button.show_recent_thread_workspaces"
	ActionShowThreads                 ActionKind = "surface.button.show_threads"
	ActionShowAllThreads              ActionKind = "surface.button.show_all_threads"
	ActionShowScopedThreads           ActionKind = "surface.button.show_scoped_threads"
	ActionShowWorkspaceThreads        ActionKind = "surface.button.show_workspace_threads"
	ActionUseThread                   ActionKind = "surface.button.use_thread"
	ActionThreadSelectionPage         ActionKind = "surface.thread_selection.page"
	ActionConfirmKickThread           ActionKind = "surface.button.confirm_kick_thread"
	ActionCancelKickThread            ActionKind = "surface.button.cancel_kick_thread"
	ActionFollowLocal                 ActionKind = "surface.button.follow_local"
	ActionDetach                      ActionKind = "surface.button.detach"
	ActionVSCodeMigrate               ActionKind = "surface.button.vscode_migrate"
	ActionPathPickerEnter             ActionKind = "surface.path_picker.enter"
	ActionPathPickerUp                ActionKind = "surface.path_picker.up"
	ActionPathPickerSelect            ActionKind = "surface.path_picker.select"
	ActionPathPickerPage              ActionKind = "surface.path_picker.page"
	ActionPathPickerConfirm           ActionKind = "surface.path_picker.confirm"
	ActionPathPickerCancel            ActionKind = "surface.path_picker.cancel"
	ActionTargetPickerSelectWorkspace ActionKind = "surface.target_picker.select_workspace"
	ActionTargetPickerSelectSession   ActionKind = "surface.target_picker.select_session"
	ActionTargetPickerPage            ActionKind = "surface.target_picker.page"
	ActionTargetPickerOpenPathPicker  ActionKind = "surface.target_picker.open_path_picker"
	ActionTargetPickerCancel          ActionKind = "surface.target_picker.cancel"
	ActionTargetPickerConfirm         ActionKind = "surface.target_picker.confirm"
	ActionHistoryPage                 ActionKind = "surface.history.page"
	ActionHistoryDetail               ActionKind = "surface.history.detail"
	ActionFeishuAppTestCallback       ActionKind = "surface.button.feishu_app_test_callback"
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
	CardCallback          bool
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
	SteerInputs         []agentproto.Input
	RequestAnswers      map[string][]string
	OptionID            string
	Request             *ActionRequestResponse
	RequestControl      *ActionRequestControl
	OwnerFlow           *ActionOwnerCardFlow
	LocalPageAction     bool
	CatalogFamilyID     string
	CatalogVariantID    string
	CatalogBackend      agentproto.Backend
	CommandID           string
	ParentCommandText   string
	InstanceID          string
	WorkspaceKey        string
	ThreadID            string
	TurnID              string
	ViewMode            string
	Page                int
	ReturnPage          int
	Cursor              int
	PickerID            string
	FieldName           string
	PickerEntry         string
	TargetPickerValue   string
	AllowCrossWorkspace bool
	LocalPath           string
	FileName            string
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

type Snapshot struct {
	SurfaceSessionID  string
	ActorUserID       string
	ProductMode       string
	Backend           agentproto.Backend
	WorkspaceKey      string
	CodexProviderID   string
	ClaudeProfileID   string
	ClaudeProfileName string
	Attachment        AttachmentSummary
	PendingHeadless   PendingHeadlessSummary
	NextPrompt        PromptRouteSummary
	Gate              GateSummary
	Dispatch          DispatchSummary
	AutoWhip          AutoWhipSummary
	AutoContinue      AutoContinueSummary
	PermissionGaps    []PermissionGapSummary
	Instances         []InstanceSummary
	Threads           []ThreadSummary
}

type PermissionGapSummary struct {
	Scope        string
	ScopeType    string
	ApplyURL     string
	SourceAPI    string
	ErrorCode    int
	FirstSeenAt  time.Time
	LastSeenAt   time.Time
	LastVerified time.Time
	HitCount     int
}

type AttachmentSummary struct {
	InstanceID                     string
	ObjectType                     string
	DisplayName                    string
	Source                         string
	Managed                        bool
	PID                            int
	SelectedThreadID               string
	SelectedThreadTitle            string
	SelectedThreadFirstUserMessage string
	SelectedThreadLastUserMessage  string
	SelectedThreadModelReroute     *agentproto.TurnModelReroute
	SelectedThreadAgeText          string
	RouteMode                      string
	Abandoning                     bool
}

type PendingHeadlessSummary struct {
	InstanceID            string
	ThreadID              string
	ThreadTitle           string
	ThreadCWD             string
	Backend               agentproto.Backend
	CodexProviderID       string
	ClaudeProfileID       string
	ClaudeReasoningEffort string
	Status                string
	PID                   int
	ExpiresAt             time.Time
	RequestedAt           time.Time
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
	OverridePlanMode               string
	PlanModeOverrideSet            bool
	UsesLocalRequestedOverrides    bool
	EffectivePlanMode              string
	ObservedThreadAccessMode       string
	ObservedThreadPlanMode         string
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

type AutoWhipSummary struct {
	Enabled             bool
	PendingReason       string
	PendingDueAt        time.Time
	ConsecutiveCount    int
	LastTriggeredTurnID string
}

type AutoContinueSummary struct {
	Enabled                    bool
	State                      string
	PendingDueAt               time.Time
	AttemptCount               int
	ConsecutiveDryFailureCount int
	TriggerKind                string
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
	ThreadID           string
	Name               string
	DisplayTitle       string
	Preview            string
	CWD                string
	State              string
	RuntimeStatus      string
	Model              string
	ReasoningEffort    string
	LastModelReroute   *agentproto.TurnModelReroute
	Loaded             bool
	WaitingOnApproval  bool
	WaitingOnUserInput bool
	IsObservedFocused  bool
	IsSelected         bool
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
	Code                  string
	Title                 string
	TemporarySessionLabel string
	Text                  string
	ThemeKey              string
	Sections              []FeishuCardTextSection
	DeliveryClass         NoticeDeliveryClass
	DeliveryFamily        NoticeDeliveryFamily
	DeliveryDedupKey      string
}

type ThreadSelectionChanged struct {
	ThreadID  string
	RouteMode string
	Title     string
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
	Answered       bool
	Skipped        bool
	Optional       bool
	AllowOther     bool
	Secret         bool
	Options        []RequestPromptQuestionOption
	Placeholder    string
	DefaultValue   string
	DirectResponse bool
}

type CommandCatalogButtonKind string

const (
	CommandCatalogButtonAction         CommandCatalogButtonKind = "action"
	CommandCatalogButtonCallbackAction CommandCatalogButtonKind = "callback_action"
	CommandCatalogButtonOpenURL        CommandCatalogButtonKind = "open_url"
)

type CommandCatalogFormFieldKind string

const (
	CommandCatalogFormFieldText         CommandCatalogFormFieldKind = "text"
	CommandCatalogFormFieldSelectStatic CommandCatalogFormFieldKind = "select_static"
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
	Label            string
	Kind             CommandCatalogButtonKind
	CommandText      string
	CommandID        string
	CatalogFamilyID  string
	CatalogVariantID string
	CatalogBackend   agentproto.Backend
	OpenURL          string
	CallbackValue    map[string]any
	Style            string
	Disabled         bool
}

type CommandCatalogFormFieldOption struct {
	Label string
	Value string
}

type CommandCatalogFormField struct {
	Name         string
	Kind         CommandCatalogFormFieldKind
	Label        string
	Placeholder  string
	DefaultValue string
	Options      []CommandCatalogFormFieldOption
}

type CommandCatalogForm struct {
	CommandID        string
	CommandText      string
	CatalogFamilyID  string
	CatalogVariantID string
	CatalogBackend   agentproto.Backend
	SubmitLabel      string
	SubmitValue      map[string]any
	Field            CommandCatalogFormField
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

type TurnDiffSnapshot struct {
	ThreadID string
	TurnID   string
	Diff     string
}

type TurnDiffPreview struct {
	URL string
}

type FinalTurnUsage struct {
	InputTokens           int
	CachedInputTokens     int
	OutputTokens          int
	ReasoningOutputTokens int
	TotalTokens           int
}

type FinalTurnSummary struct {
	Elapsed              time.Duration
	ThreadCWD            string
	Usage                *FinalTurnUsage
	ThreadUsage          *FinalTurnUsage
	TotalTokensInContext int
	ContextInputTokens   *int
	ModelContextWindow   *int
}

type TimelineTextType string

const (
	TimelineTextSteerUserSupplement TimelineTextType = "steer_user_supplement"
)

type TimelineText struct {
	ThreadID              string
	TurnID                string
	Type                  TimelineTextType
	Text                  string
	ReplyToMessageID      string
	ReplyToMessagePreview string
}

type ImageOutput struct {
	ThreadID    string
	TurnID      string
	ItemID      string
	Prompt      string
	SavedPath   string
	ImageBase64 string
}

type ExecCommandProgressFileChange struct {
	Path         string
	MovePath     string
	Kind         string
	Diff         string
	AddedLines   int
	RemovedLines int
}

type ExecCommandProgressTimelineItem struct {
	ID         string
	Kind       string
	Label      string
	Items      []string
	Summary    string
	Secondary  string
	Status     string
	FileChange *ExecCommandProgressFileChange
	LastSeq    int
}

type ExecCommandProgressSegment struct {
	SegmentID string
	MessageID string
	StartSeq  int
	EndSeq    int
}

type ExecCommandProgress struct {
	ThreadID              string
	TurnID                string
	ItemID                string
	TemporarySessionLabel string
	ActiveSegmentID       string
	Segments              []ExecCommandProgressSegment
	Verbosity             string
	Timeline              []ExecCommandProgressTimelineItem
}

type DaemonCommandKind string

const (
	DaemonCommandStartHeadless              DaemonCommandKind = "headless.start"
	DaemonCommandKillHeadless               DaemonCommandKind = "headless.kill"
	DaemonCommandAdmin                      DaemonCommandKind = "admin.command"
	DaemonCommandDebug                      DaemonCommandKind = "debug.command"
	DaemonCommandCron                       DaemonCommandKind = "cron.command"
	DaemonCommandUpgrade                    DaemonCommandKind = "upgrade.command"
	DaemonCommandUpgradeOwnerFlow           DaemonCommandKind = "upgrade.owner_flow"
	DaemonCommandVSCodeMigrateCommand       DaemonCommandKind = "vscode.migrate.command"
	DaemonCommandVSCodeMigrate              DaemonCommandKind = "vscode.migrate"
	DaemonCommandThreadHistoryRead          DaemonCommandKind = "thread.history.read"
	DaemonCommandSendIMFile                 DaemonCommandKind = "feishu.im_file.send"
	DaemonCommandGitWorkspaceImport         DaemonCommandKind = "workspace.git_import"
	DaemonCommandGitWorkspaceImportCancel   DaemonCommandKind = "workspace.git_import.cancel"
	DaemonCommandGitWorkspaceWorktreeCreate DaemonCommandKind = "workspace.git_worktree.create"
	DaemonCommandGitWorkspaceWorktreeCancel DaemonCommandKind = "workspace.git_worktree.cancel"
)

type DaemonCommand struct {
	Kind                  DaemonCommandKind
	GatewayID             string
	SurfaceSessionID      string
	SourceMessageID       string
	FromCardAction        bool
	PickerID              string
	OptionID              string
	InstanceID            string
	ThreadID              string
	ThreadTitle           string
	ThreadCWD             string
	Backend               agentproto.Backend
	CodexProviderID       string
	ClaudeProfileID       string
	ClaudeReasoningEffort string
	WorkspaceKey          string
	AutoRestore           bool
	Text                  string
	LocalPath             string
	RepoURL               string
	RefName               string
	BranchName            string
	DirectoryName         string
}
