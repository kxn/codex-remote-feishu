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

type TurnCompletionOrigin string

const (
	TurnCompletionOriginRuntime              TurnCompletionOrigin = "runtime"
	TurnCompletionOriginTurnStartRejected    TurnCompletionOrigin = "turn_start_rejected"
	TurnCompletionOriginThreadStartRejected  TurnCompletionOrigin = "thread_start_rejected"
	TurnCompletionOriginThreadResumeRejected TurnCompletionOrigin = "thread_resume_rejected"
)

type EventKind string

const (
	EventThreadsSnapshot               EventKind = "threads.snapshot"
	EventThreadHistoryRead             EventKind = "thread.history.read"
	EventThreadDiscovered              EventKind = "thread.discovered"
	EventThreadFocused                 EventKind = "thread.focused"
	EventThreadRuntimeStatusUpdated    EventKind = "thread.runtime_status.updated"
	EventProcessChildRestartUpdated    EventKind = "process.child.restart.updated"
	EventConfigObserved                EventKind = "config.observed"
	EventLocalInteractionObserved      EventKind = "local.interaction.observed"
	EventThreadTokenUsageUpdated       EventKind = "thread.token_usage.updated"
	EventTurnDiffUpdated               EventKind = "turn.diff.updated"
	EventTurnModelRerouted             EventKind = "turn.model_rerouted"
	EventTurnPlanUpdated               EventKind = "turn.plan.updated"
	EventTurnStarted                   EventKind = "turn.started"
	EventTurnCompleted                 EventKind = "turn.completed"
	EventItemStarted                   EventKind = "item.started"
	EventItemDelta                     EventKind = "item.delta"
	EventItemTerminalInteraction       EventKind = "item.command_execution.terminal_interaction"
	EventItemReasoningSummaryPartAdded EventKind = "item.reasoning.summary_part_added"
	EventItemFileChangePatchUpdated    EventKind = "item.file_change.patch_updated"
	EventItemCompleted                 EventKind = "item.completed"
	EventMCPOAuthLoginURLReady         EventKind = "mcp.oauth_login.authorization_url"
	EventMCPOAuthLoginCompleted        EventKind = "mcp.oauth_login.completed"
	EventRequestStarted                EventKind = "request.started"
	EventRequestResolved               EventKind = "request.resolved"
	EventModelCatalogUpdated           EventKind = "models.catalog.updated"
	EventSystemError                   EventKind = "system.error"
)

type ChildRestartStatus string

const (
	ChildRestartStatusSucceeded ChildRestartStatus = "succeeded"
	ChildRestartStatusFailed    ChildRestartStatus = "failed"
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
	Seq                  uint64                   `json:"seq,omitempty"`
	Kind                 EventKind                `json:"kind"`
	CommandID            string                   `json:"commandId,omitempty"`
	ThreadID             string                   `json:"threadId,omitempty"`
	TurnID               string                   `json:"turnId,omitempty"`
	TurnCompletionOrigin TurnCompletionOrigin     `json:"turnCompletionOrigin,omitempty"`
	ItemID               string                   `json:"itemId,omitempty"`
	RequestID            string                   `json:"requestId,omitempty"`
	Status               string                   `json:"status,omitempty"`
	ErrorMessage         string                   `json:"errorMessage,omitempty"`
	CWD                  string                   `json:"cwd,omitempty"`
	FocusSource          string                   `json:"focusSource,omitempty"`
	Action               string                   `json:"action,omitempty"`
	ItemKind             string                   `json:"itemKind,omitempty"`
	Delta                string                   `json:"delta,omitempty"`
	TurnDiff             string                   `json:"turnDiff,omitempty"`
	Name                 string                   `json:"name,omitempty"`
	Preview              string                   `json:"preview,omitempty"`
	Model                string                   `json:"model,omitempty"`
	ReasoningEffort      string                   `json:"reasoningEffort,omitempty"`
	AccessMode           string                   `json:"accessMode,omitempty"`
	PlanMode             string                   `json:"planMode,omitempty"`
	ObservedPermission   *ObservedPermissionState `json:"observedPermission,omitempty"`
	ConfigScope          string                   `json:"configScope,omitempty"`
	Loaded               bool                     `json:"loaded,omitempty"`
	Archived             bool                     `json:"archived,omitempty"`
	TrafficClass         TrafficClass             `json:"trafficClass,omitempty"`
	Initiator            Initiator                `json:"initiator,omitempty"`
	Problem              *ErrorInfo               `json:"problem,omitempty"`
	RequestPrompt        *RequestPrompt           `json:"requestPrompt,omitempty"`
	MCPOAuthLogin        *MCPOAuthLoginEvent      `json:"mcpOAuthLogin,omitempty"`
	MCPToolProgress      *MCPToolCallProgress     `json:"mcpToolProgress,omitempty"`
	ApprovalReview       *AutoApprovalReview      `json:"approvalReview,omitempty"`
	TokenUsage           *ThreadTokenUsage        `json:"tokenUsage,omitempty"`
	ModelReroute         *TurnModelReroute        `json:"modelReroute,omitempty"`
	ModelCatalog         *ModelCatalogSnapshot    `json:"modelCatalog,omitempty"`
	PlanSnapshot         *TurnPlanSnapshot        `json:"planSnapshot,omitempty"`
	ThreadHistory        *ThreadHistoryRecord     `json:"threadHistory,omitempty"`
	RuntimeStatus        *ThreadRuntimeStatus     `json:"runtimeStatus,omitempty"`
	Metadata             map[string]any           `json:"metadata,omitempty"`
	Threads              []ThreadSnapshotRecord   `json:"threads,omitempty"`
	FileChanges          []FileChangeRecord       `json:"fileChanges,omitempty"`
}

type ThreadSnapshotRecord struct {
	ThreadID           string                   `json:"threadId"`
	ForkedFromID       string                   `json:"forkedFromId,omitempty"`
	Source             *ThreadSourceRecord      `json:"source,omitempty"`
	Name               string                   `json:"name,omitempty"`
	Preview            string                   `json:"preview,omitempty"`
	WorkspaceKey       string                   `json:"workspaceKey,omitempty"`
	CWD                string                   `json:"cwd,omitempty"`
	Model              string                   `json:"model,omitempty"`
	ReasoningEffort    string                   `json:"reasoningEffort,omitempty"`
	AccessMode         string                   `json:"accessMode,omitempty"`
	PlanMode           string                   `json:"planMode,omitempty"`
	ObservedPermission *ObservedPermissionState `json:"observedPermission,omitempty"`
	Loaded             bool                     `json:"loaded"`
	Archived           bool                     `json:"archived"`
	State              string                   `json:"state,omitempty"`
	ListOrder          int                      `json:"listOrder,omitempty"`
	RuntimeStatus      *ThreadRuntimeStatus     `json:"runtimeStatus,omitempty"`
}

type ThreadHistoryRecord struct {
	Thread ThreadSnapshotRecord      `json:"thread"`
	Turns  []ThreadHistoryTurnRecord `json:"turns,omitempty"`
}

type ThreadHistoryTurnRecord struct {
	TurnID       string                    `json:"turnId"`
	Status       string                    `json:"status,omitempty"`
	StartedAt    time.Time                 `json:"startedAt,omitempty"`
	CompletedAt  time.Time                 `json:"completedAt,omitempty"`
	ErrorMessage string                    `json:"errorMessage,omitempty"`
	RequestID    string                    `json:"requestId,omitempty"`
	Items        []ThreadHistoryItemRecord `json:"items,omitempty"`
}

type ThreadHistoryItemRecord struct {
	ItemID   string         `json:"itemId"`
	Kind     string         `json:"kind,omitempty"`
	Status   string         `json:"status,omitempty"`
	Text     string         `json:"text,omitempty"`
	Command  string         `json:"command,omitempty"`
	CWD      string         `json:"cwd,omitempty"`
	ExitCode *int           `json:"exitCode,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type CommandKind string

const (
	CommandPromptSend          CommandKind = "prompt.send"
	CommandReviewStart         CommandKind = "review.start"
	CommandThreadCompactStart  CommandKind = "thread.compact.start"
	CommandTurnSteer           CommandKind = "turn.steer"
	CommandTurnInterrupt       CommandKind = "turn.interrupt"
	CommandRequestRespond      CommandKind = "request.respond"
	CommandMCPOAuthLogin       CommandKind = "mcp.oauth_login.start"
	CommandThreadsRefresh      CommandKind = "threads.refresh"
	CommandThreadHistoryRead   CommandKind = "thread.history.read"
	CommandModelList           CommandKind = "model.list"
	CommandProcessChildRestart CommandKind = "process.child.restart"
	CommandProcessExit         CommandKind = "process.exit"
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
	CommandID string           `json:"commandId,omitempty"`
	IssuedAt  time.Time        `json:"issuedAt,omitempty"`
	Kind      CommandKind      `json:"kind"`
	Origin    Origin           `json:"origin"`
	Target    Target           `json:"target"`
	Prompt    Prompt           `json:"prompt,omitempty"`
	Overrides PromptOverrides  `json:"overrides,omitempty"`
	Request   Request          `json:"request,omitempty"`
	ModelList ModelListCommand `json:"modelList,omitempty"`
	MCP       MCPCommand       `json:"mcp,omitempty"`
	Review    ReviewRequest    `json:"review,omitempty"`
}

type ModelListCommand struct {
	Cursor        string `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	IncludeHidden bool   `json:"includeHidden,omitempty"`
}

type ModelCatalogSnapshot struct {
	Entries       []ModelCatalogEntry `json:"entries,omitempty"`
	NextCursor    string              `json:"nextCursor,omitempty"`
	IncludeHidden bool                `json:"includeHidden,omitempty"`
	Unsupported   bool                `json:"unsupported,omitempty"`
	ErrorMessage  string              `json:"errorMessage,omitempty"`
	RefreshedAt   time.Time           `json:"refreshedAt,omitempty"`
}

type ModelCatalogEntry struct {
	ID                        string                  `json:"id,omitempty"`
	Model                     string                  `json:"model,omitempty"`
	DisplayName               string                  `json:"displayName,omitempty"`
	Description               string                  `json:"description,omitempty"`
	Hidden                    bool                    `json:"hidden,omitempty"`
	SupportedReasoningEfforts []ReasoningEffortOption `json:"supportedReasoningEfforts,omitempty"`
	DefaultReasoningEffort    string                  `json:"defaultReasoningEffort,omitempty"`
	ServiceTiers              []ModelServiceTier      `json:"serviceTiers,omitempty"`
	DefaultServiceTier        string                  `json:"defaultServiceTier,omitempty"`
	Upgrade                   string                  `json:"upgrade,omitempty"`
	UpgradeInfo               *ModelUpgradeInfo       `json:"upgradeInfo,omitempty"`
	AvailabilityMessage       string                  `json:"availabilityMessage,omitempty"`
	IsDefault                 bool                    `json:"isDefault,omitempty"`
}

type ReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	Description     string `json:"description,omitempty"`
}

type ModelServiceTier struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type ModelUpgradeInfo struct {
	Model             string `json:"model,omitempty"`
	UpgradeCopy       string `json:"upgradeCopy,omitempty"`
	ModelLink         string `json:"modelLink,omitempty"`
	MigrationMarkdown string `json:"migrationMarkdown,omitempty"`
}

type Origin struct {
	Surface   string `json:"surface,omitempty"`
	UserID    string `json:"userId,omitempty"`
	ChatID    string `json:"chatId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
}

type Target struct {
	// Legacy prompt-dispatch carrier:
	// - canonical semantics now live in PromptDispatchPlan and the
	//   PromptDispatchPlanFromTarget/LegacyTarget boundary translator.
	// - these fields remain on Target for queue/runtime compatibility until the
	//   later carrier migration lands.
	ExecutionMode          PromptExecutionMode  `json:"executionMode,omitempty"`
	SourceThreadID         string               `json:"sourceThreadId,omitempty"`
	SurfaceBindingPolicy   SurfaceBindingPolicy `json:"surfaceBindingPolicy,omitempty"`
	ThreadID               string               `json:"threadId,omitempty"`
	CreateThreadIfMissing  bool                 `json:"createThreadIfMissing,omitempty"`
	InternalHelper         bool                 `json:"internalHelper,omitempty"`
	CWD                    string               `json:"cwd,omitempty"`
	TurnID                 string               `json:"turnId,omitempty"`
	UseActiveTurnIfOmitted bool                 `json:"useActiveTurnIfOmitted,omitempty"`
}

type Prompt struct {
	Inputs []Input `json:"inputs,omitempty"`
}

type PromptOverrides struct {
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	AccessMode      string `json:"accessMode,omitempty"`
	PlanMode        string `json:"planMode,omitempty"`
}

type Request struct {
	RequestID          string         `json:"requestId,omitempty"`
	Response           map[string]any `json:"response,omitempty"`
	BridgeKind         string         `json:"bridgeKind,omitempty"`
	SemanticKind       string         `json:"semanticKind,omitempty"`
	InterruptOnDecline bool           `json:"interruptOnDecline,omitempty"`
}

type MCPCommand struct {
	OAuthLogin *MCPOAuthLoginCommand `json:"oauthLogin,omitempty"`
}

type MCPOAuthLoginCommand struct {
	ServerName  string   `json:"serverName,omitempty"`
	ThreadID    string   `json:"threadId,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	TimeoutSecs int      `json:"timeoutSecs,omitempty"`
}

type MCPOAuthLoginEvent struct {
	ServerName       string   `json:"serverName,omitempty"`
	ThreadID         string   `json:"threadId,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	TimeoutSecs      int      `json:"timeoutSecs,omitempty"`
	AuthorizationURL string   `json:"authorizationUrl,omitempty"`
	Success          bool     `json:"success,omitempty"`
	Error            string   `json:"error,omitempty"`
}

type RequestType string

const (
	RequestTypeApproval                   RequestType = "approval"
	RequestTypeRequestUserInput           RequestType = "request_user_input"
	RequestTypePermissionsRequestApproval RequestType = "permissions_request_approval"
	RequestTypeMCPServerElicitation       RequestType = "mcp_server_elicitation"
	RequestTypeToolCallback               RequestType = "tool_callback"
	RequestTypeUnsupportedServerRequest   RequestType = "unsupported_server_request"
)

type RequestOption struct {
	OptionID string `json:"optionId,omitempty"`
	Label    string `json:"label,omitempty"`
	Style    string `json:"style,omitempty"`
}

type RequestQuestionOption struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

type RequestQuestion struct {
	ID             string                  `json:"id,omitempty"`
	Header         string                  `json:"header,omitempty"`
	Question       string                  `json:"question,omitempty"`
	AllowOther     bool                    `json:"allowOther,omitempty"`
	Secret         bool                    `json:"secret,omitempty"`
	Options        []RequestQuestionOption `json:"options,omitempty"`
	Placeholder    string                  `json:"placeholder,omitempty"`
	DefaultValue   string                  `json:"defaultValue,omitempty"`
	DirectResponse bool                    `json:"directResponse,omitempty"`
}

type PermissionsRequestPrompt struct {
	Reason      string           `json:"reason,omitempty"`
	Permissions []map[string]any `json:"permissions,omitempty"`
}

type MCPElicitationPrompt struct {
	ServerName      string         `json:"serverName,omitempty"`
	Mode            string         `json:"mode,omitempty"`
	Message         string         `json:"message,omitempty"`
	URL             string         `json:"url,omitempty"`
	ElicitationID   string         `json:"elicitationId,omitempty"`
	RequestedSchema map[string]any `json:"requestedSchema,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
}

type ToolCallbackPrompt struct {
	CallID     string         `json:"callId,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	Arguments  any            `json:"arguments,omitempty"`
	RawPayload map[string]any `json:"rawPayload,omitempty"`
}

type RequestPrompt struct {
	Type           RequestType               `json:"type,omitempty"`
	RawType        string                    `json:"rawType,omitempty"`
	Title          string                    `json:"title,omitempty"`
	Body           string                    `json:"body,omitempty"`
	ItemID         string                    `json:"itemId,omitempty"`
	AcceptLabel    string                    `json:"acceptLabel,omitempty"`
	DeclineLabel   string                    `json:"declineLabel,omitempty"`
	Options        []RequestOption           `json:"options,omitempty"`
	Questions      []RequestQuestion         `json:"questions,omitempty"`
	Permissions    *PermissionsRequestPrompt `json:"permissions,omitempty"`
	MCPElicitation *MCPElicitationPrompt     `json:"mcpElicitation,omitempty"`
	ToolCallback   *ToolCallbackPrompt       `json:"toolCallback,omitempty"`
}

type MCPToolCallProgress struct {
	Message string `json:"message,omitempty"`
}

type AutoApprovalReview struct {
	TargetItemID string         `json:"targetItemId,omitempty"`
	ActionType   string         `json:"actionType,omitempty"`
	Review       map[string]any `json:"review,omitempty"`
	Action       map[string]any `json:"action,omitempty"`
}
