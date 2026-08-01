package codex

import (
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
		ThreadID:  threadID,
		CWD:       cwd,
	}
	params := map[string]any{
		"threadId": threadID,
		"cwd":      cwd,
	}
	applyCodexResumePolicyToThreadParams(params, t.childRestartRestorePolicy)
	payload := map[string]any{
		"id":     requestID,
		"method": "thread/resume",
		"params": params,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		delete(t.pendingChildRestartRestore, requestID)
		return nil, "", false, err
	}
	return append(bytes, '\n'), requestID, true, nil
}

func (t *Translator) PrepareChildRestartRestorePolicy(policy *agentproto.CodexResumePolicy) {
	t.childRestartRestorePolicy = agentproto.CloneCodexResumePolicy(policy)
}

func (t *Translator) CancelChildRestartRestore(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	delete(t.pendingChildRestartRestore, requestID)
}
