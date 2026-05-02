package state

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
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
	// ProductModeNormal is the persisted token for the headless runtime shape.
	// User-visible mode names should usually be projected as codex / claude / vscode.
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

func IsHeadlessProductMode(mode ProductMode) bool {
	return NormalizeProductMode(mode) == ProductModeNormal
}

func IsVSCodeProductMode(mode ProductMode) bool {
	return NormalizeProductMode(mode) == ProductModeVSCode
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

type PlanModeSetting string

const (
	PlanModeSettingOff PlanModeSetting = "off"
	PlanModeSettingOn  PlanModeSetting = "on"
)

func NormalizePlanModeSetting(value PlanModeSetting) PlanModeSetting {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "on", "plan":
		return PlanModeSettingOn
	default:
		return PlanModeSettingOff
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
	QueueItemSourceAutoWhip     QueueItemSourceKind = "auto_whip"
	QueueItemSourceAutoContinue QueueItemSourceKind = "auto_continue"
)

type ImageState string

const (
	ImageStaged    ImageState = "staged"
	ImageCancelled ImageState = "cancelled"
	ImageBound     ImageState = "bound"
	ImageDiscarded ImageState = "discarded"
)

type FileState string

const (
	FileStaged    FileState = "staged"
	FileCancelled FileState = "cancelled"
	FileBound     FileState = "bound"
	FileDiscarded FileState = "discarded"
)

type AutoWhipReason string

const (
	AutoWhipReasonIncompleteStop AutoWhipReason = "incomplete_stop"
)

type AutoContinueEpisodeState string

const (
	AutoContinueEpisodeScheduled AutoContinueEpisodeState = "scheduled"
	AutoContinueEpisodeRunning   AutoContinueEpisodeState = "running"
	AutoContinueEpisodeCompleted AutoContinueEpisodeState = "completed"
	AutoContinueEpisodeFailed    AutoContinueEpisodeState = "failed"
	AutoContinueEpisodeCancelled AutoContinueEpisodeState = "cancelled"
)

type AutoContinueTriggerKind string

const (
	AutoContinueTriggerKindEligibleFailure AutoContinueTriggerKind = "autocontinue_eligible_failure"
)

type SurfaceMessageKind string

const (
	SurfaceMessageKindText  SurfaceMessageKind = "text"
	SurfaceMessageKindCard  SurfaceMessageKind = "card"
	SurfaceMessageKindImage SurfaceMessageKind = "image"
)

type Root struct {
	Instances                       map[string]*InstanceRecord
	Surfaces                        map[string]*SurfaceConsoleRecord
	WorkspaceDefaults               map[string]ModelConfigRecord
	CodexProviders                  map[string]CodexProviderRecord
	ClaudeProfiles                  map[string]ClaudeProfileRecord
	ClaudeWorkspaceProfileSnapshots map[string]ClaudeWorkspaceProfileSnapshotRecord
}

type ModelConfigRecord struct {
	Model           string
	ReasoningEffort string
	AccessMode      string
}

type ClaudeWorkspaceProfileSnapshotRecord struct {
	ReasoningEffort string
	AccessMode      string
	PlanMode        PlanModeSetting
}

type InstanceRecord struct {
	InstanceID              string
	DisplayName             string
	WorkspaceRoot           string
	WorkspaceKey            string
	ShortName               string
	Backend                 agentproto.Backend
	CodexProviderID         string
	ClaudeProfileID         string
	Source                  string
	Capabilities            agentproto.Capabilities
	CapabilitiesDeclared    bool
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
	ForkedFromID            string
	Source                  *agentproto.ThreadSourceRecord
	Name                    string
	Preview                 string
	FirstUserMessage        string
	LastUserMessage         string
	LastAssistantMessage    string
	CWD                     string
	State                   string
	RuntimeStatus           *agentproto.ThreadRuntimeStatus
	ExplicitModel           string
	ExplicitReasoningEffort string
	ObservedPlanMode        PlanModeSetting
	LastModelReroute        *agentproto.TurnModelReroute
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
	Kind                 ThreadReplayKind
	TurnID               string
	ItemID               string
	Text                 string
	SourceMessageID      string
	SourceMessagePreview string
	NoticeCode           string
	NoticeTitle          string
	NoticeText           string
	NoticeThemeKey       string
}

type SurfaceConsoleRecord struct {
	SurfaceSessionID string
	Platform         string
	GatewayID        string
	ChatID           string
	ActorUserID      string
	// ProductMode carries the outer runtime shape: headless vs vscode.
	// Backend carries the inner provider choice inside that shape.
	ProductMode          ProductMode
	Backend              agentproto.Backend
	CodexProviderID      string
	ClaudeProfileID      string
	Verbosity            SurfaceVerbosity
	PlanMode             PlanModeSetting
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
	StagedFiles          map[string]*StagedFileRecord
	QueueItems           map[string]*QueueItemRecord
	PreparedThreadCWD    string
	PreparedFromThreadID string
	PreparedAt           time.Time
	PromptOverride       ModelConfigRecord
	PendingHeadless      *HeadlessLaunchRecord
	PendingRequests      map[string]*RequestPromptRecord
	ActiveRequestCapture *RequestCaptureRecord
	ActiveExecProgress   *ExecCommandProgressRecord
	RecentFinalCards     []*FinalCardRecord
	SurfaceMessageSeq    int
	SurfaceMessages      map[string]*SurfaceMessageRecord
	LastThreadHistory    *agentproto.ThreadHistoryRecord
	LastSelection        *SelectionAnnouncementRecord
	AutoWhip             AutoWhipRuntimeRecord
	AutoContinue         AutoContinueRuntimeRecord
	ReviewSession        *ReviewSessionRecord
}

type ReviewSessionPhase string

const (
	ReviewSessionPhasePending ReviewSessionPhase = "pending"
	ReviewSessionPhaseActive  ReviewSessionPhase = "active"
)

type ReviewSessionRecord struct {
	Phase           ReviewSessionPhase
	ParentThreadID  string
	ReviewThreadID  string
	ActiveTurnID    string
	ThreadCWD       string
	SourceMessageID string
	TargetLabel     string
	LastReviewText  string
	StartedAt       time.Time
	LastUpdatedAt   time.Time
}

type ExecCommandProgressEntryRecord struct {
	ItemID     string
	Kind       string
	Label      string
	Summary    string
	Status     string
	FileChange *ExecCommandProgressFileChangeRecord
	LastSeq    int
}

type ExecCommandProgressFileChangeRecord struct {
	Path         string
	MovePath     string
	Kind         string
	Diff         string
	AddedLines   int
	RemovedLines int
}

type ExecCommandProgressBlockRowRecord struct {
	RowID     string
	Kind      string
	Items     []string
	Summary   string
	Secondary string
	MergeKey  string
	LastSeq   int
}

type ExecCommandProgressBlockRecord struct {
	BlockID string
	Kind    string
	Status  string
	Rows    []ExecCommandProgressBlockRowRecord
}

type ExecCommandProgressExplorationRecord struct {
	Block         ExecCommandProgressBlockRecord
	ActiveItemIDs map[string]bool
	Failed        bool
}

type ExecCommandProgressPlanRecord struct {
	Block    ExecCommandProgressBlockRecord
	Snapshot *agentproto.TurnPlanSnapshot
}

type ExecCommandProgressSegmentRecord struct {
	SegmentID string
	MessageID string
	StartSeq  int
	EndSeq    int
}

type ExecCommandProgressRecord struct {
	InstanceID           string
	ThreadID             string
	TurnID               string
	ItemID               string
	ActiveSegmentID      string
	Segments             []ExecCommandProgressSegmentRecord
	Verbosity            SurfaceVerbosity
	Entries              []ExecCommandProgressEntryRecord
	Commands             []string
	Command              string
	CWD                  string
	Status               string
	Exploration          *ExecCommandProgressExplorationRecord
	ProcessPlan          *ExecCommandProgressPlanRecord
	Reasoning            *ExecCommandProgressReasoningRecord
	DynamicToolItemGroup map[string]string
	DynamicToolGroups    map[string]*DynamicToolProgressGroupRecord
	LastVisibleSeq       int
	LastEmittedAt        time.Time
}

type ExecCommandProgressReasoningRecord struct {
	ItemID              string
	Text                string
	VisibleSummaryIndex int
	Buffer              string
	BufferSummaryIndex  int
	LastUpdatedAt       time.Time
	Revision            int64
	LastEmittedRevision int64
}

type DynamicToolProgressGroupRecord struct {
	GroupKey string
	Tool     string
	Label    string
	Args     []string
	Summary  string
	Status   string
}

type AutoWhipRuntimeRecord struct {
	Enabled                      bool
	PendingReason                AutoWhipReason
	PendingDueAt                 time.Time
	ConsecutiveCount             int
	LastTriggeredTurnID          string
	PendingReplyToMessageID      string
	PendingReplyToMessagePreview string
	IncompleteStopCount          int
	SuppressOnce                 bool
}

type AutoContinueRuntimeRecord struct {
	Enabled bool
	Episode *PendingAutoContinueEpisodeRecord
}

type PendingAutoContinueEpisodeRecord struct {
	EpisodeID                  string
	InstanceID                 string
	ThreadID                   string
	FrozenCWD                  string
	FrozenExecutionMode        agentproto.PromptExecutionMode
	FrozenSourceThreadID       string
	FrozenSurfaceBindingPolicy agentproto.SurfaceBindingPolicy
	FrozenRouteMode            RouteMode
	FrozenOverride             ModelConfigRecord
	FrozenPlanMode             PlanModeSetting
	RootReplyToMessageID       string
	RootReplyToMessagePreview  string
	NoticeMessageID            string
	NoticeAppendSeq            int
	State                      AutoContinueEpisodeState
	TriggerKind                AutoContinueTriggerKind
	AttemptCount               int
	ConsecutiveDryFailureCount int
	PendingDueAt               time.Time
	LastTurnID                 string
	LastProblem                *agentproto.ErrorInfo
	CurrentAttemptOutputSeen   bool
}

type SurfaceMessageRecord struct {
	MessageID        string
	Kind             SurfaceMessageKind
	ReplyToMessageID string
	AppendSeq        int
	RecordedAt       time.Time
}

type HeadlessLaunchStatus string

const (
	HeadlessLaunchStarting HeadlessLaunchStatus = "starting"
)

type HeadlessLaunchPurpose string

const (
	HeadlessLaunchPurposeThreadRestore  HeadlessLaunchPurpose = "thread_restore"
	HeadlessLaunchPurposeFreshWorkspace HeadlessLaunchPurpose = "fresh_workspace"
)

type HeadlessLaunchRecord struct {
	InstanceID       string
	ThreadID         string
	ThreadTitle      string
	ThreadCWD        string
	Backend          agentproto.Backend
	CodexProviderID  string
	ClaudeProfileID  string
	ThreadName       string
	ThreadPreview    string
	RequestedAt      time.Time
	ExpiresAt        time.Time
	Status           HeadlessLaunchStatus
	Purpose          HeadlessLaunchPurpose
	PrepareNewThread bool
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
	Optional       bool
	AllowOther     bool
	Secret         bool
	Options        []RequestPromptQuestionOptionRecord
	Placeholder    string
	DefaultValue   string
	DirectResponse bool
}

type RequestPromptTextSectionRecord struct {
	Label string
	Lines []string
}

func (s RequestPromptTextSectionRecord) Normalized() RequestPromptTextSectionRecord {
	lines := make([]string, 0, len(s.Lines))
	for _, line := range s.Lines {
		for _, part := range strings.Split(line, "\n") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
	}
	return RequestPromptTextSectionRecord{
		Label: strings.TrimSpace(s.Label),
		Lines: lines,
	}
}

type RequestPromptRecord struct {
	RequestID                string
	RequestType              string
	SemanticKind             string
	Backend                  agentproto.Backend
	Prompt                   *agentproto.RequestPrompt
	InstanceID               string
	ThreadID                 string
	TurnID                   string
	SourceMessageID          string
	ItemID                   string
	Title                    string
	Sections                 []RequestPromptTextSectionRecord
	Options                  []RequestPromptOptionRecord
	Questions                []RequestPromptQuestionRecord
	CurrentQuestionIndex     int
	HintText                 string
	LocalKind                string
	LocalMeta                map[string]string
	DraftAnswers             map[string]string
	SkippedQuestionIDs       map[string]bool
	CardRevision             int
	Phase                    frontstagecontract.Phase
	PendingDispatchCommandID string
	CreatedAt                time.Time
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
	ID                    string
	SurfaceSessionID      string
	SourceKind            QueueItemSourceKind
	AutoContinueEpisodeID string
	SourceMessageID       string
	SourceMessagePreview  string
	SourceMessageIDs      []string
	ReplyToMessageID      string
	ReplyToMessagePreview string
	Inputs                []agentproto.Input
	SteerInputs           []agentproto.Input
	RestoreAsStagedImage  bool
	FrozenThreadID        string
	FrozenCWD             string
	// #429 execution carrier; runtime/product follow-ups are in #430/#428.
	FrozenExecutionMode        agentproto.PromptExecutionMode
	FrozenSourceThreadID       string
	FrozenSurfaceBindingPolicy agentproto.SurfaceBindingPolicy
	FrozenOverride             ModelConfigRecord
	FrozenPlanMode             PlanModeSetting
	RouteModeAtEnqueue         RouteMode
	Status                     QueueItemStatus
}

type StagedImageRecord struct {
	ImageID          string
	SurfaceSessionID string
	SourceMessageID  string
	ActorUserID      string
	LocalPath        string
	MIMEType         string
	State            ImageState
}

type StagedFileRecord struct {
	FileID           string
	SurfaceSessionID string
	SourceMessageID  string
	ActorUserID      string
	LocalPath        string
	FileName         string
	State            FileState
}

func NewRoot() *Root {
	return &Root{
		Instances:                       map[string]*InstanceRecord{},
		Surfaces:                        map[string]*SurfaceConsoleRecord{},
		WorkspaceDefaults:               map[string]ModelConfigRecord{},
		CodexProviders:                  map[string]CodexProviderRecord{},
		ClaudeProfiles:                  map[string]ClaudeProfileRecord{},
		ClaudeWorkspaceProfileSnapshots: map[string]ClaudeWorkspaceProfileSnapshotRecord{},
	}
}
