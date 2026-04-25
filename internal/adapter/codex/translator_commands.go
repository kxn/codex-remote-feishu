package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	switch command.Kind {
	case agentproto.CommandPromptSend:
		if command.Target.ThreadID == "" {
			t.pendingLocalNewThreadTurn = false
			requestID := t.nextRequest("thread-start")
			t.pendingThreadCreate[requestID] = pendingThreadCreate{Command: command}
			params := t.buildThreadStartParams(command.Target.CWD, command.Overrides)
			if command.Target.InternalHelper {
				params["ephemeral"] = true
				params["persistExtendedHistory"] = false
				t.pendingInternalThreadSet[requestID] = true
			}
			t.debugf(
				"translate remote prompt: command=%s action=thread/start request=%s targetThread=%s cwd=%s currentThread=%s surface=%s inputs=%d",
				command.CommandID,
				requestID,
				command.Target.ThreadID,
				command.Target.CWD,
				t.currentThreadID,
				choose(command.Origin.Surface, command.Origin.ChatID),
				len(command.Prompt.Inputs),
			)
			payload := map[string]any{
				"id":     requestID,
				"method": "thread/start",
				"params": params,
			}
			bytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return [][]byte{append(bytes, '\n')}, nil
		}
		delete(t.pendingLocalTurnByThread, command.Target.ThreadID)
		if t.currentThreadID == "" || command.Target.ThreadID != t.currentThreadID {
			requestID := t.nextRequest("thread-resume")
			t.pendingThreadResume[requestID] = pendingThreadResume{
				ThreadID: command.Target.ThreadID,
				Command:  command,
			}
			t.debugf(
				"translate remote prompt: command=%s action=thread/resume request=%s targetThread=%s cwd=%s currentThread=%s knownCWD=%s surface=%s inputs=%d",
				command.CommandID,
				requestID,
				command.Target.ThreadID,
				command.Target.CWD,
				t.currentThreadID,
				t.knownThreadCWD[command.Target.ThreadID],
				choose(command.Origin.Surface, command.Origin.ChatID),
				len(command.Prompt.Inputs),
			)
			payload := map[string]any{
				"id":     requestID,
				"method": "thread/resume",
				"params": map[string]any{
					"threadId": command.Target.ThreadID,
					"cwd":      choose(command.Target.CWD, t.knownThreadCWD[command.Target.ThreadID]),
				},
			}
			bytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return [][]byte{append(bytes, '\n')}, nil
		}
		payload, requestID, err := t.directTurnStart(command.Target.ThreadID, command, false)
		if err != nil {
			return nil, err
		}
		t.debugf(
			"translate remote prompt: command=%s action=turn/start request=%s targetThread=%s cwd=%s currentThread=%s surface=%s inputs=%d",
			command.CommandID,
			requestID,
			command.Target.ThreadID,
			command.Target.CWD,
			t.currentThreadID,
			choose(command.Origin.Surface, command.Origin.ChatID),
			len(command.Prompt.Inputs),
		)
		return [][]byte{payload}, nil
	case agentproto.CommandThreadCompactStart:
		threadID := strings.TrimSpace(command.Target.ThreadID)
		if threadID == "" {
			return nil, fmt.Errorf("thread.compact.start requires thread id")
		}
		if t.currentThreadID == "" || threadID != t.currentThreadID {
			requestID := t.nextRequest("thread-resume")
			t.pendingThreadResume[requestID] = pendingThreadResume{
				ThreadID: threadID,
				Command:  command,
			}
			t.debugf(
				"translate remote compact: command=%s action=thread/resume request=%s targetThread=%s currentThread=%s knownCWD=%s surface=%s",
				command.CommandID,
				requestID,
				threadID,
				t.currentThreadID,
				t.knownThreadCWD[threadID],
				choose(command.Origin.Surface, command.Origin.ChatID),
			)
			payload := map[string]any{
				"id":     requestID,
				"method": "thread/resume",
				"params": map[string]any{
					"threadId": threadID,
					"cwd":      choose(command.Target.CWD, t.knownThreadCWD[threadID]),
				},
			}
			bytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return [][]byte{append(bytes, '\n')}, nil
		}
		payload, requestID, err := t.directCompactStart(command)
		if err != nil {
			return nil, err
		}
		t.debugf(
			"translate remote compact: command=%s action=thread/compact/start request=%s targetThread=%s currentThread=%s surface=%s",
			command.CommandID,
			requestID,
			threadID,
			t.currentThreadID,
			choose(command.Origin.Surface, command.Origin.ChatID),
		)
		return [][]byte{payload}, nil
	case agentproto.CommandTurnInterrupt:
		payload := map[string]any{
			"id":     t.nextRequest("turn-interrupt"),
			"method": "turn/interrupt",
			"params": map[string]any{
				"threadId": command.Target.ThreadID,
				"turnId":   command.Target.TurnID,
			},
		}
		t.pendingSuppressedResponse[lookupStringFromAny(payload["id"])] = suppressedResponseContext{Action: "turn/interrupt"}
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandTurnSteer:
		payload := map[string]any{
			"id":     t.nextRequest("turn-steer"),
			"method": "turn/steer",
			"params": map[string]any{
				"threadId":       command.Target.ThreadID,
				"expectedTurnId": command.Target.TurnID,
				"input":          t.buildInputs(command.Prompt.Inputs),
			},
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandThreadsRefresh:
		if t.startupThreadListBorrowSatisfied {
			t.startupThreadListBorrowArmed = false
			t.startupThreadListBorrowSatisfied = false
			t.startupThreadListBorrowRequestID = ""
			t.debugf(
				"translate threads refresh: startup borrow already satisfied currentThread=%s inflightReads=%d",
				t.currentThreadID,
				len(t.pendingThreadReads),
			)
			return nil, nil
		}
		if borrowedRequestID, ok := t.consumeStartupThreadListBorrow(); ok {
			t.pendingThreadListRequestID = borrowedRequestID
			t.pendingThreadListBorrowed = true
			t.debugf(
				"translate threads refresh: borrow request=%s currentThread=%s inflightReads=%d",
				borrowedRequestID,
				t.currentThreadID,
				len(t.pendingThreadReads),
			)
			return nil, nil
		}
		requestID := t.nextRequest("threads-refresh")
		t.pendingThreadListRequestID = requestID
		t.pendingThreadListBorrowed = false
		t.debugf(
			"translate threads refresh: request=%s currentThread=%s inflightReads=%d",
			requestID,
			t.currentThreadID,
			len(t.pendingThreadReads),
		)
		payload := map[string]any{
			"id":     requestID,
			"method": "thread/list",
			"params": map[string]any{
				"limit":          50,
				"cursor":         nil,
				"sortKey":        "created_at",
				"modelProviders": []any{},
				"archived":       false,
				"sourceKinds":    []any{},
			},
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandThreadHistoryRead:
		requestID := t.nextRequest("thread-history-read")
		t.pendingThreadHistoryReads[requestID] = pendingThreadHistoryRead{
			CommandID: command.CommandID,
			ThreadID:  command.Target.ThreadID,
		}
		payload := map[string]any{
			"id":     requestID,
			"method": "thread/read",
			"params": map[string]any{
				"threadId":     command.Target.ThreadID,
				"includeTurns": true,
			},
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandRequestRespond:
		return t.translateRequestRespond(command)
	default:
		return nil, nil
	}
}

func (t *Translator) translateRequestRespond(command agentproto.Command) ([][]byte, error) {
	if command.Request.RequestID == "" {
		return nil, nil
	}
	result := map[string]any{}
	responseType, _ := command.Request.Response["type"].(string)
	switch responseType {
	case "approval":
		if decision, _ := command.Request.Response["decision"].(string); strings.TrimSpace(decision) != "" {
			result["decision"] = strings.TrimSpace(decision)
			break
		}
		approved, _ := command.Request.Response["approved"].(bool)
		if approved {
			result["decision"] = "accept"
		} else {
			result["decision"] = "decline"
		}
	case "structured":
		if value, ok := command.Request.Response["result"]; ok {
			result = map[string]any{"result": value}
		}
	default:
		result = command.Request.Response
	}
	payload := map[string]any{
		"id":     decodeNativeRequestID(command.Request.RequestID),
		"result": result,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return [][]byte{append(bytes, '\n')}, nil
}

func (t *Translator) buildThreadStartParams(cwd string, overrides agentproto.PromptOverrides) map[string]any {
	params := cloneMap(t.latestThreadStartParams)
	if len(params) == 0 {
		params = map[string]any{}
	}
	params["cwd"] = choose(cwd, lookupStringFromAny(params["cwd"]))
	setDefault(params, "model", nil)
	setDefault(params, "modelProvider", nil)
	setDefault(params, "config", map[string]any{})
	setDefault(params, "approvalPolicy", "on-request")
	setDefault(params, "baseInstructions", nil)
	setDefault(params, "developerInstructions", nil)
	setDefault(params, "sandbox", "read-only")
	setDefault(params, "personality", nil)
	setDefault(params, "experimentalRawEvents", false)
	setDefault(params, "dynamicTools", nil)
	applyPromptOverridesToThreadStart(params, overrides)
	return params
}

func (t *Translator) directTurnStart(threadID string, command agentproto.Command, newThread bool) ([]byte, string, error) {
	delete(t.pendingLocalTurnByThread, threadID)
	t.pendingRemoteTurnByThread[threadID] = choose(command.Origin.Surface, command.Origin.ChatID)
	template := t.selectTurnTemplate(threadID, newThread)
	template["threadId"] = threadID
	template["input"] = t.buildInputs(command.Prompt.Inputs)
	template["cwd"] = choose(command.Target.CWD, choose(lookupStringFromAny(template["cwd"]), t.knownThreadCWD[threadID]))
	setDefault(template, "approvalPolicy", nil)
	setDefault(template, "sandboxPolicy", nil)
	setDefault(template, "model", nil)
	setDefault(template, "effort", nil)
	setDefault(template, "summary", "auto")
	setDefault(template, "personality", nil)
	setDefault(template, "collaborationMode", nil)
	setDefault(template, "attachments", []any{})
	applyPromptOverridesToTurnStart(template, command.Overrides)
	requestID := t.nextRequest("turn-start")
	payload := map[string]any{
		"id":     requestID,
		"method": "turn/start",
		"params": template,
	}
	t.pendingSuppressedResponse[lookupStringFromAny(payload["id"])] = suppressedResponseContext{
		Action:           "turn/start",
		ThreadID:         threadID,
		SurfaceSessionID: choose(command.Origin.Surface, command.Origin.ChatID),
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return append(bytes, '\n'), requestID, nil
}

func (t *Translator) directCompactStart(command agentproto.Command) ([]byte, string, error) {
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if threadID == "" {
		return nil, "", fmt.Errorf("thread.compact.start requires thread id")
	}
	requestID := t.nextRequest("thread-compact-start")
	surfaceID := choose(command.Origin.Surface, command.Origin.ChatID)
	t.pendingRemoteTurnByThread[threadID] = surfaceID
	payload := map[string]any{
		"id":     requestID,
		"method": "thread/compact/start",
		"params": map[string]any{
			"threadId": threadID,
		},
	}
	t.pendingSuppressedResponse[requestID] = suppressedResponseContext{
		Action:           "thread/compact/start",
		ThreadID:         threadID,
		SurfaceSessionID: surfaceID,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return append(bytes, '\n'), requestID, nil
}

func (t *Translator) selectTurnTemplate(threadID string, newThread bool) map[string]any {
	switch {
	case newThread && len(t.newThreadTurnTemplate) > 0:
		return cloneMap(t.newThreadTurnTemplate)
	case len(t.turnStartByThread[threadID]) > 0:
		return cloneMap(t.turnStartByThread[threadID])
	case len(t.latestTurnStartTemplate) > 0:
		return cloneMap(t.latestTurnStartTemplate)
	default:
		return map[string]any{}
	}
}

func (t *Translator) buildInputs(inputs []agentproto.Input) []map[string]any {
	output := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			output = append(output, map[string]any{"type": "text", "text": input.Text, "text_elements": []any{}})
		case agentproto.InputLocalImage:
			output = append(output, map[string]any{"type": "localImage", "path": input.Path, "mimeType": input.MIMEType})
		case agentproto.InputRemoteImage:
			output = append(output, map[string]any{"type": "image", "url": input.URL, "mimeType": input.MIMEType})
		}
	}
	return output
}

func (t *Translator) ArmStartupThreadListBorrow() {
	t.startupThreadListBorrowArmed = true
	t.startupThreadListBorrowSatisfied = false
	t.startupThreadListBorrowRequestID = ""
}

func (t *Translator) consumeStartupThreadListBorrow() (string, bool) {
	requestID := strings.TrimSpace(t.startupThreadListBorrowRequestID)
	t.startupThreadListBorrowArmed = false
	t.startupThreadListBorrowSatisfied = false
	t.startupThreadListBorrowRequestID = ""
	if requestID == "" {
		return "", false
	}
	return requestID, true
}

func (t *Translator) nextRequest(prefix string) string {
	value := fmt.Sprintf("relay-%s-%d", prefix, t.nextID)
	t.nextID++
	return value
}
