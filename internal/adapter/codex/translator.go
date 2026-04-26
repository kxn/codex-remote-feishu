package codex

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

type Translator struct {
	instanceID                 string
	nextID                     int
	debugLog                   func(string, ...any)
	currentThreadID            string
	knownThreadCWD             map[string]string
	pendingRemoteTurnByThread  map[string]string
	pendingLocalTurnByThread   map[string]bool
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

	latestThreadStartParams map[string]any
	latestTurnStartTemplate map[string]any
	turnStartByThread       map[string]map[string]any
	newThreadTurnTemplate   map[string]any

	threadListBroker          *threadListBroker
	threadListRefresh         *threadListRefreshSession
	pendingThreadHistoryReads map[string]pendingThreadHistoryRead
	pendingSuppressedResponse map[string]suppressedResponseContext
	pendingRequestTypes       map[string]agentproto.RequestType
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
	ThreadID string
	CWD      string
}

type pendingThreadHistoryRead struct {
	CommandID string
	ThreadID  string
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
		pendingRemoteTurnByThread:  map[string]string{},
		pendingLocalTurnByThread:   map[string]bool{},
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
	}
}

func (t *Translator) SetDebugLogger(debugLog func(string, ...any)) {
	t.debugLog = debugLog
}

func (t *Translator) debugf(format string, args ...any) {
	if t.debugLog != nil {
		t.debugLog(format, args...)
	}
}
