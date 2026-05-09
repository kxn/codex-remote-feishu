package claude

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func buildAgentQuestions(questions []pendingQuestion) []agentproto.RequestQuestion {
	out := make([]agentproto.RequestQuestion, 0, len(questions))
	for _, question := range questions {
		out = append(out, agentproto.RequestQuestion{
			ID:       question.ID,
			Header:   question.Header,
			Question: question.Question,
		})
	}
	return out
}

func requestResultMetadata(request *pendingRequest, message, block map[string]any) map[string]any {
	metadata := map[string]any{
		"requestType": string(request.RequestType),
		"tool":        request.ToolName,
	}
	switch rawToolResult := message["tool_use_result"].(type) {
	case map[string]any:
		for key, value := range rawToolResult {
			metadata[key] = cloneJSONValue(value)
		}
	case string:
		if strings.TrimSpace(rawToolResult) != "" {
			metadata["toolUseResult"] = strings.TrimSpace(rawToolResult)
		}
	}
	if request.Decision != "" {
		metadata["decision"] = request.Decision
	}
	if contentText := stringifyTextContent(block["content"]); strings.TrimSpace(contentText) != "" {
		metadata["text"] = strings.TrimSpace(contentText)
	}
	if request.ToolName == "AskUserQuestion" && len(request.Questions) != 0 {
		metadata["questions"] = buildQuestionMetadata(buildAgentQuestions(request.Questions))
	}
	if request.ToolName == "ExitPlanMode" {
		if request.PlanBody != "" {
			metadata["body"] = request.PlanBody
		}
		if request.PlanBodySource != "" {
			metadata["planBodySource"] = request.PlanBodySource
		}
	}
	return metadata
}

func (t *Translator) resolvePendingRequestForToolResult(message, block map[string]any, toolUseID string) (agentproto.Event, bool) {
	if t.activeTurn == nil {
		return agentproto.Event{}, false
	}
	request := findRequestByToolUseID(t.pendingRequests, toolUseID)
	if request == nil {
		return agentproto.Event{}, false
	}
	delete(t.pendingRequests, request.RequestID)
	return agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		CommandID: t.activeTurn.CommandID,
		ThreadID:  request.ThreadID,
		TurnID:    request.TurnID,
		RequestID: request.RequestID,
		Metadata:  requestResultMetadata(request, message, block),
	}, true
}
