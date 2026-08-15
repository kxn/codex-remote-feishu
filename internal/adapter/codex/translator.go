package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/adapter/adapterkit"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type Translator struct {
	adapterkit.TranslatorBase
	instanceID                 string
	currentThreadID            string
	knownThreadCWD             map[string]string
	observedThreads            map[string]codexObservedThread
	pendingRemoteTurnByThread  map[string]string
	pendingLocalTurnByThread   map[string]bool
	pendingCodexPolicyByThread map[string]*agentproto.CodexResumePolicy
	pendingLocalNewThreadTurn  bool
	pendingTurnProblems        map[string]agentproto.ErrorInfo
	pendingThreadCreate        map[string]pendingThreadCreate
	pendingThreadResume        map[string]pendingThreadResume
	pendingReviewStart         map[string]pendingReviewStart
	pendingReviewThreads       map[string]pendingReviewThread
	pendingThreadNameSet       map[string]pendingThreadNameSet
	pendingChildRestartRestore map[string]pendingChildRestartRestore
	pendingInternalThreadSet   map[string]bool
	pendingInternalTurnSet     map[string]bool
	internalThreadIDs          map[string]bool
	internalTurnIDs            map[string]bool
	turnInitiators             map[string]agentproto.Initiator
	suppressedThreadStarted    map[string]bool
	childRestartRestorePolicy  *agentproto.CodexResumePolicy

	latestThreadStartParams map[string]any
	latestTurnStartTemplate map[string]any
	turnStartByThread       map[string]map[string]any
	newThreadTurnTemplate   map[string]any

	threadListBroker          *threadListBroker
	threadListRefresh         *threadListRefreshSession
	pendingThreadHistoryReads map[string]pendingThreadHistoryRead
	pendingSuppressedResponse map[string]suppressedResponseContext
	pendingRequestTypes       map[string]agentproto.RequestType
	pendingMCPOAuthLogins     map[string]pendingMCPOAuthLogin
	pendingMCPOAuthLoginKeys  map[string]string
	pendingModelList          map[string]pendingModelList
	pendingGoalRequests       map[string]pendingGoalRequest
	pendingThreadReads        map[string]pendingThreadRead
	lastOwnedGoalMutations    map[string]ownedGoalMutation
	reasoningSummaryIndexes   map[string]map[int]bool
}

type pendingThreadCreate struct {
	Command agentproto.Command
	Action  string
}

type pendingThreadResume struct {
	ThreadID string
	Command  agentproto.Command
}

type pendingReviewStart struct {
	ThreadID  string
	Initiator agentproto.Initiator
}

type pendingReviewThread struct {
	ParentThreadID string
	Initiator      agentproto.Initiator
}

type pendingThreadNameSet struct {
	ThreadID string
	Name     string
}

type pendingChildRestartRestore struct {
	CommandID string
	ThreadID  string
	CWD       string
}

type pendingThreadHistoryRead struct {
	CommandID string
	ThreadID  string
}

type pendingMCPOAuthLogin struct {
	CommandID        string
	Initiator        agentproto.Initiator
	ServerName       string
	ThreadID         string
	Scopes           []string
	TimeoutSecs      int
	AuthorizationURL string
}

type pendingModelList struct {
	CommandID     string
	IncludeHidden bool
}

type pendingGoalRequest struct {
	CommandID string
	ThreadID  string
	Operation string
	Purpose   string
}

type pendingThreadRead struct {
	CommandID string
	ThreadID  string
}

type ownedGoalMutation struct {
	UpdatedAt int64
	Cleared   bool
	AwaitNext bool
}

type codexObservedThread struct {
	ModelProviderID string
	Model           string
	ReasoningEffort string
}

type suppressedResponseContext struct {
	Action           string
	ThreadID         string
	SurfaceSessionID string
}

type Result struct {
	Events           []agentproto.Event
	OutboundToCodex  [][]byte
	OutboundToParent [][]byte
	Suppress         bool
}

func NewTranslator(instanceID string) *Translator {
	return &Translator{
		instanceID:                 instanceID,
		knownThreadCWD:             map[string]string{},
		observedThreads:            map[string]codexObservedThread{},
		pendingRemoteTurnByThread:  map[string]string{},
		pendingLocalTurnByThread:   map[string]bool{},
		pendingCodexPolicyByThread: map[string]*agentproto.CodexResumePolicy{},
		pendingTurnProblems:        map[string]agentproto.ErrorInfo{},
		pendingThreadCreate:        map[string]pendingThreadCreate{},
		pendingThreadResume:        map[string]pendingThreadResume{},
		pendingReviewStart:         map[string]pendingReviewStart{},
		pendingReviewThreads:       map[string]pendingReviewThread{},
		pendingThreadNameSet:       map[string]pendingThreadNameSet{},
		pendingChildRestartRestore: map[string]pendingChildRestartRestore{},
		pendingInternalThreadSet:   map[string]bool{},
		pendingInternalTurnSet:     map[string]bool{},
		internalThreadIDs:          map[string]bool{},
		internalTurnIDs:            map[string]bool{},
		turnInitiators:             map[string]agentproto.Initiator{},
		suppressedThreadStarted:    map[string]bool{},
		turnStartByThread:          map[string]map[string]any{},
		threadListBroker:           newThreadListBroker(),
		pendingThreadHistoryReads:  map[string]pendingThreadHistoryRead{},
		pendingSuppressedResponse:  map[string]suppressedResponseContext{},
		pendingRequestTypes:        map[string]agentproto.RequestType{},
		pendingMCPOAuthLogins:      map[string]pendingMCPOAuthLogin{},
		pendingMCPOAuthLoginKeys:   map[string]string{},
		pendingModelList:           map[string]pendingModelList{},
		pendingGoalRequests:        map[string]pendingGoalRequest{},
		pendingThreadReads:         map[string]pendingThreadRead{},
		lastOwnedGoalMutations:     map[string]ownedGoalMutation{},
		reasoningSummaryIndexes:    map[string]map[int]bool{},
	}
}
