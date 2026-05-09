package wrapper

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
)

type commandResponseTracker struct {
	mu      sync.Mutex
	pending map[string]pendingCommandResponse
}

type pendingCommandResponse struct {
	ch       chan *agentproto.ErrorInfo
	problem  agentproto.ErrorInfo
	suppress bool
}

func newCommandResponseTracker() *commandResponseTracker {
	return &commandResponseTracker{
		pending: map[string]pendingCommandResponse{},
	}
}

func (t *commandResponseTracker) Register(requestID string, problem agentproto.ErrorInfo, suppress bool) <-chan *agentproto.ErrorInfo {
	if strings.TrimSpace(requestID) == "" {
		return nil
	}
	ch := make(chan *agentproto.ErrorInfo, 1)
	t.mu.Lock()
	t.pending[requestID] = pendingCommandResponse{
		ch:       ch,
		problem:  problem.Normalize(),
		suppress: suppress,
	}
	t.mu.Unlock()
	return ch
}

func (t *commandResponseTracker) Cancel(requestID string) {
	if strings.TrimSpace(requestID) == "" {
		return
	}
	t.mu.Lock()
	delete(t.pending, requestID)
	t.mu.Unlock()
}

func (t *commandResponseTracker) ResolveFrame(line []byte) (bool, bool) {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return false, false
	}
	requestID := lookupStringFromMap(message, "id")
	if strings.TrimSpace(requestID) == "" {
		return false, false
	}
	return t.resolve(requestID, extractJSONRPCErrorMessage(message))
}

func (t *commandResponseTracker) ResolveRequestID(requestID, rejectMessage string) (bool, bool) {
	return t.resolve(requestID, rejectMessage)
}

func (t *commandResponseTracker) resolve(requestID, rejectMessage string) (bool, bool) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return false, false
	}
	t.mu.Lock()
	pending, ok := t.pending[requestID]
	if ok {
		delete(t.pending, requestID)
	}
	t.mu.Unlock()
	if !ok {
		return false, false
	}

	var problem *agentproto.ErrorInfo
	if errMsg := strings.TrimSpace(rejectMessage); errMsg != "" {
		value := pending.problem
		value.Details = firstNonEmpty(errMsg, value.Details)
		problem = &value
	}
	pending.ch <- problem
	close(pending.ch)
	return true, pending.suppress
}

func waitCommandResponse(ctx context.Context, ch <-chan *agentproto.ErrorInfo, timeout time.Duration, timeoutProblem agentproto.ErrorInfo) error {
	if ch == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case problem, ok := <-ch:
		if !ok || problem == nil {
			return nil
		}
		return *problem
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return timeoutProblem.Normalize()
	}
}

func extractJSONRPCErrorMessage(message map[string]any) string {
	return jsonrpcutil.ExtractErrorMessage(message)
}

func lookupStringFromRawFrame(line []byte, key string) string {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return ""
	}
	return lookupStringFromMap(message, key)
}
