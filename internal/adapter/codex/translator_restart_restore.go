package codex

import (
	"encoding/json"
	"strings"
)

func (t *Translator) BuildChildRestartRestoreFrame(commandID string) ([]byte, string, bool, error) {
	threadID := strings.TrimSpace(t.currentThreadID)
	if threadID == "" {
		return nil, "", false, nil
	}
	cwd := strings.TrimSpace(t.knownThreadCWD[threadID])
	requestID := t.nextRequest("child-restart-restore")
	t.pendingChildRestartRestore[requestID] = pendingChildRestartRestore{
		CommandID: strings.TrimSpace(commandID),
		ThreadID: threadID,
		CWD:      cwd,
	}
	payload := map[string]any{
		"id":     requestID,
		"method": "thread/resume",
		"params": map[string]any{
			"threadId": threadID,
			"cwd":      cwd,
		},
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		delete(t.pendingChildRestartRestore, requestID)
		return nil, "", false, err
	}
	return append(bytes, '\n'), requestID, true, nil
}

func (t *Translator) CancelChildRestartRestore(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	delete(t.pendingChildRestartRestore, requestID)
}
