package codex

import (
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/translator"
)

// Compile-time check that *Translator satisfies the translator.Translator interface.
var _ translator.Translator = (*Translator)(nil)

type Translator struct {
	instanceID                string
	nextID                    int
	debugLog                  func(string, ...any)
	currentThreadID           string
	knownThreadCWD            map[string]string
	pendingRemoteTurnByThread map[string]string
	pendingLocalTurnByThread  map[string]bool
	pendingLocalNewThreadTurn bool
	pendingTurnProblems       map[string]agentproto.ErrorInfo
	pendingThreadCreate       map[string]pendingThreadCreate
	pendingThreadResume       map[string]pendingThreadResume
	pendingThreadNameSet      map[string]pendingThreadNameSet
	pendingInternalThreadSet  map[string]bool
	pendingInternalTurnSet    map[string]bool
	internalThreadIDs         map[string]bool
	internalTurnIDs           map[string]bool
	turnInitiators            map[string]agentproto.Initiator

	latestThreadStartParams map[string]any
	latestTurnStartTemplate map[string]any
	turnStartByThread       map[string]map[string]any
	newThreadTurnTemplate   map[string]any

	pendingThreadListRequestID string
	pendingThreadReads         map[string]string
	threadRefreshRecords       map[string]agentproto.ThreadSnapshotRecord
	threadRefreshOrder         []string
	pendingSuppressedResponse  map[string]suppressedResponseContext
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

type suppressedResponseContext struct {
	Action   string
	ThreadID string
}

// Result is an alias for the canonical translator.Result type.
type Result = translator.Result

func NewTranslator(instanceID string) *Translator {
	return &Translator{
		instanceID:                instanceID,
		knownThreadCWD:            map[string]string{},
		pendingRemoteTurnByThread: map[string]string{},
		pendingLocalTurnByThread:  map[string]bool{},
		pendingTurnProblems:       map[string]agentproto.ErrorInfo{},
		pendingThreadCreate:       map[string]pendingThreadCreate{},
		pendingThreadResume:       map[string]pendingThreadResume{},
		pendingThreadNameSet:      map[string]pendingThreadNameSet{},
		pendingInternalThreadSet:  map[string]bool{},
		pendingInternalTurnSet:    map[string]bool{},
		internalThreadIDs:         map[string]bool{},
		internalTurnIDs:           map[string]bool{},
		turnInitiators:            map[string]agentproto.Initiator{},
		turnStartByThread:         map[string]map[string]any{},
		pendingThreadReads:        map[string]string{},
		threadRefreshRecords:      map[string]agentproto.ThreadSnapshotRecord{},
		pendingSuppressedResponse: map[string]suppressedResponseContext{},
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

// BootstrapFrames returns synthetic frames to send to the Codex child process
// on startup. For headless sources, this sends a JSON-RPC initialize request.
func (t *Translator) BootstrapFrames(source string, version string) ([][]byte, error) {
	if !strings.EqualFold(strings.TrimSpace(source), "headless") {
		return nil, nil
	}
	if version == "" {
		version = "dev"
	}
	payload := map[string]any{
		"id":     "relay-bootstrap-initialize",
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "Codex Remote Headless",
				"title":   "Codex Remote Headless",
				"version": version,
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		},
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return [][]byte{append(bytes, '\n')}, nil
}
