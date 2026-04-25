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

	pendingThreadListRequestID       string
	pendingThreadListBorrowed        bool
	pendingThreadReads               map[string]string
	threadRefreshRecords             map[string]agentproto.ThreadSnapshotRecord
	threadRefreshOrder               []string
	startupThreadListBorrowArmed     bool
	startupThreadListBorrowSatisfied bool
	startupThreadListBorrowRequestID string
	pendingThreadHistoryReads        map[string]pendingThreadHistoryRead
	pendingSuppressedResponse        map[string]suppressedResponseContext
	pendingRequestTypes              map[string]agentproto.RequestType
}

type pendingThreadCreate struct {
	Command agentproto.Command
}

type pendingThreadResume struct {
	ThreadID string
	Command  agentproto.Command
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
	Events          []agentproto.Event
	OutboundToCodex [][]byte
	Suppress        bool
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
		pendingThreadNameSet:       map[string]pendingThreadNameSet{},
		pendingChildRestartRestore: map[string]pendingChildRestartRestore{},
		pendingInternalThreadSet:   map[string]bool{},
		pendingInternalTurnSet:     map[string]bool{},
		internalThreadIDs:          map[string]bool{},
		internalTurnIDs:            map[string]bool{},
		turnInitiators:             map[string]agentproto.Initiator{},
		suppressedThreadStarted:    map[string]bool{},
		turnStartByThread:          map[string]map[string]any{},
		pendingThreadReads:         map[string]string{},
		threadRefreshRecords:       map[string]agentproto.ThreadSnapshotRecord{},
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
