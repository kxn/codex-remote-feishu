package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func commandInitiator(command agentproto.Command) agentproto.Initiator {
	surfaceID := strings.TrimSpace(firstNonEmptyString(command.Origin.Surface, command.Origin.ChatID))
	if surfaceID == "" {
		return agentproto.Initiator{Kind: agentproto.InitiatorUnknown}
	}
	return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID}
}

func (t *Translator) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	switch command.Kind {
	case agentproto.CommandPromptSend:
		return t.translatePromptSend(command)
	case agentproto.CommandTurnSteer:
		return t.translateTurnSteer(command)
	case agentproto.CommandTurnInterrupt:
		return t.translateInterrupt(command)
	case agentproto.CommandRequestRespond:
		return t.translateRequestRespond(command)
	default:
		return nil, agentproto.ErrorInfo{
			Code:             "claude_command_not_supported_yet",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "当前 Claude runtime 只支持 prompt.send / turn.interrupt / request.respond。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
}

func (t *Translator) translatePromptSend(command agentproto.Command) ([][]byte, error) {
	text, err := t.requireTextPromptInputs(
		command,
		"claude_prompt_inputs_unsupported",
		"当前 Claude runtime 第一版只支持纯文本 prompt.send。",
	)
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if t.sessionID != "" {
		threadID = t.canonicalThreadID(threadID)
	}
	turn := &turnState{
		CommandID: command.CommandID,
		Initiator: commandInitiator(command),
		ThreadID:  threadID,
		TurnID:    t.nextTurnID(),
	}
	t.pendingTurns = append(t.pendingTurns, turn)

	outbound := make([][]byte, 0, 2)
	if frame, ok, err := t.buildPermissionModeFrame(command.Overrides.AccessMode, command.Overrides.PlanMode); err != nil {
		return nil, err
	} else if ok {
		outbound = append(outbound, frame)
	}

	frame, err := marshalUserTextFrame(text)
	if err != nil {
		return nil, err
	}
	outbound = append(outbound, frame)
	return outbound, nil
}

func (t *Translator) translateTurnSteer(command agentproto.Command) ([][]byte, error) {
	if t.activeTurn == nil {
		return nil, agentproto.ErrorInfo{
			Code:             "claude_steer_requires_active_turn",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "Claude 只有在当前存在 active turn 时才能并入文本补充。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
	if targetThread := strings.TrimSpace(command.Target.ThreadID); targetThread != "" && targetThread != t.activeTurn.ThreadID {
		return nil, agentproto.ErrorInfo{
			Code:             "claude_steer_turn_mismatch",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "当前 Claude active turn 已变化，无法把补充并入目标轮次。",
			Details:          fmt.Sprintf("expected active thread id %q, got %q", t.activeTurn.ThreadID, targetThread),
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
	if targetTurn := strings.TrimSpace(command.Target.TurnID); targetTurn != "" && targetTurn != t.activeTurn.TurnID {
		return nil, agentproto.ErrorInfo{
			Code:             "claude_steer_turn_mismatch",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "当前 Claude active turn 已变化，无法把补充并入目标轮次。",
			Details:          fmt.Sprintf("expected active turn id %q, got %q", t.activeTurn.TurnID, targetTurn),
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
	text, err := t.requireTextPromptInputs(
		command,
		"claude_steer_inputs_unsupported",
		"Claude 当前只能把纯文本补充并入 active turn。",
	)
	if err != nil {
		return nil, err
	}
	frame, err := marshalUserTextFrame(text)
	if err != nil {
		return nil, err
	}
	return [][]byte{frame}, nil
}

func (t *Translator) buildPermissionModeFrame(accessMode, planMode string) ([]byte, bool, error) {
	desired := claudePermissionSelectionFromOverrides(accessMode, planMode).NativeMode
	if strings.TrimSpace(t.permissionMode) == desired {
		return nil, false, nil
	}
	requestID := t.nextNativeID("set-permission-mode")
	payload := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request": map[string]any{
			"subtype": "set_permission_mode",
			"mode":    desired,
		},
	}
	frame, err := marshalNDJSON(payload)
	if err != nil {
		return nil, false, err
	}
	t.pendingControlReplies[requestID] = pendingControlReply{
		Kind:                  "set_permission_mode",
		DesiredPermissionMode: desired,
	}
	return frame, true, nil
}

func (t *Translator) translateInterrupt(command agentproto.Command) ([][]byte, error) {
	if t.activeTurn != nil {
		targetThread := strings.TrimSpace(command.Target.ThreadID)
		targetTurn := strings.TrimSpace(command.Target.TurnID)
		if targetThread == "" || targetThread == t.activeTurn.ThreadID {
			if targetTurn == "" || targetTurn == t.activeTurn.TurnID {
				t.activeTurn.InterruptRequested = true
			}
		}
	}
	requestID := t.nextNativeID("interrupt")
	payload := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request": map[string]any{
			"subtype": "interrupt",
		},
	}
	frame, err := marshalNDJSON(payload)
	if err != nil {
		return nil, err
	}
	t.pendingControlReplies[requestID] = pendingControlReply{Kind: "interrupt"}
	return [][]byte{frame}, nil
}

func (t *Translator) translateRequestRespond(command agentproto.Command) ([][]byte, error) {
	requestID := strings.TrimSpace(command.Request.RequestID)
	if requestID == "" {
		return nil, nil
	}
	request := t.pendingRequests[requestID]
	if request == nil {
		return nil, agentproto.ErrorInfo{
			Code:             "claude_request_not_found",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "Claude runtime 找不到要响应的 request。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
			RequestID:        requestID,
		}
	}
	payload, err := t.buildRequestResponsePayload(request, command.Request)
	if err != nil {
		return nil, err
	}
	frame, err := marshalNDJSON(payload)
	if err != nil {
		return nil, err
	}
	request.Response = cloneMap(command.Request.Response)
	request.Decision = resolveRequestDecision(command.Request.Response)
	if command.Request.InterruptOnDecline && request.Decision == "decline" && t.activeTurn != nil {
		t.activeTurn.InterruptRequested = true
		request.InterruptOnDecline = true
	}
	return [][]byte{frame}, nil
}

func (t *Translator) buildRequestResponsePayload(request *pendingRequest, response agentproto.Request) (map[string]any, error) {
	decision := resolveRequestDecision(response.Response)
	allow := decision == "accept" || decision == "acceptForSession"
	interrupt := response.InterruptOnDecline && decision == "decline"
	reply := map[string]any{
		"type": "control_response",
	}
	body := map[string]any{}
	if allow {
		body["behavior"] = "allow"
		body["updatedPermissions"] = []any{}
		switch request.RequestType {
		case agentproto.RequestTypeRequestUserInput:
			updatedInput := cloneMap(request.Input)
			updatedInput["answers"] = requestResponseAnswers(request, response.Response)
			body["updatedInput"] = updatedInput
		case agentproto.RequestTypeApproval:
			updatedInput := cloneMap(request.Input)
			if request.SemanticKind == control.RequestSemanticPlanConfirmation {
				updatedInput["feedback"] = firstNonEmptyString(
					lookupStringFromAny(response.Response["feedback"]),
					"Approved. Execute the plan.",
				)
			}
			body["updatedInput"] = updatedInput
		default:
			body["updatedInput"] = cloneMap(request.Input)
		}
	} else {
		body["behavior"] = "deny"
		body["message"] = firstNonEmptyString(
			lookupStringFromAny(response.Response["message"]),
			lookupStringFromAny(response.Response["reason"]),
			"Request declined by user",
		)
		if interrupt {
			body["interrupt"] = true
		}
	}
	reply["response"] = map[string]any{
		"subtype":    "success",
		"request_id": request.RequestID,
		"response":   body,
	}
	return reply, nil
}

func requestResponseAnswers(request *pendingRequest, response map[string]any) map[string]any {
	answersByID := lookupMap(response, "answers")
	if len(answersByID) == 0 {
		return map[string]any{}
	}
	answers := map[string]any{}
	for _, question := range request.Questions {
		if question.ID == "" || question.Question == "" {
			continue
		}
		record := lookupMap(answersByID, question.ID)
		values := lookupStringList(record["answers"])
		if len(values) == 0 {
			continue
		}
		answers[question.Question] = values[0]
	}
	return answers
}

func (t *Translator) requireTextPromptInputs(command agentproto.Command, code, message string) (string, error) {
	text, err := flattenPromptText(command.Prompt.Inputs)
	if err == nil {
		return text, nil
	}
	return "", agentproto.ErrorInfo{
		Code:             code,
		Layer:            "wrapper",
		Stage:            "translate_command",
		Operation:        string(command.Kind),
		Message:          message,
		Details:          err.Error(),
		SurfaceSessionID: command.Origin.Surface,
		CommandID:        command.CommandID,
		ThreadID:         command.Target.ThreadID,
		TurnID:           command.Target.TurnID,
	}
}

func marshalUserTextFrame(text string) ([]byte, error) {
	return marshalNDJSON(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	})
}

func flattenPromptText(inputs []agentproto.Input) (string, error) {
	if len(inputs) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			if strings.TrimSpace(input.Text) != "" {
				parts = append(parts, input.Text)
			}
		default:
			return "", fmt.Errorf("unsupported prompt input type %q", input.Type)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func encodeMetadataMapList(values []map[string]any) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, cloneMap(value))
	}
	return out
}

func debugJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
