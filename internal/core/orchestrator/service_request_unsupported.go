package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func unsupportedServerRequestMethod(request *state.RequestPromptRecord) string {
	if request == nil {
		return ""
	}
	if method := strings.TrimSpace(request.LocalMeta["requestMethod"]); method != "" {
		return method
	}
	if request.Prompt != nil {
		if rawType := strings.TrimSpace(request.Prompt.RawType); rawType != "" {
			return rawType
		}
	}
	return strings.TrimSpace(request.RequestType)
}

func unsupportedServerRequestResultText(request *state.RequestPromptRecord) string {
	method := unsupportedServerRequestMethod(request)
	if method == "" {
		return "Unsupported Codex server request was rejected by this relay/headless client. No credentials or attestation data were generated."
	}
	return fmt.Sprintf("Unsupported Codex server request %q was rejected by this relay/headless client. No credentials or attestation data were generated.", method)
}

func buildUnsupportedServerRequestResponse(request *state.RequestPromptRecord) map[string]any {
	method := unsupportedServerRequestMethod(request)
	return map[string]any{
		"type": "structured",
		"result": map[string]any{
			"success": false,
			"error": map[string]any{
				"code":    "unsupported_server_request",
				"message": unsupportedServerRequestResultText(request),
				"method":  method,
			},
			"contentItems": []map[string]any{{
				"type": "inputText",
				"text": unsupportedServerRequestResultText(request),
			}},
		},
	}
}

func (s *Service) autoDispatchUnsupportedServerRequest(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord) []eventcontract.Event {
	if surface == nil || request == nil {
		return nil
	}
	if requestLifecycleUsesWaitingDispatchPhase(request) {
		return nil
	}
	commandID := s.nextRequestDispatchCommandID()
	markRequestSubmitting(request, commandID)
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			CommandID: commandID,
			Kind:      agentproto.CommandRequestRespond,
			Origin: agentproto.Origin{
				Surface: surface.SurfaceSessionID,
				UserID:  surface.ActorUserID,
				ChatID:  surface.ChatID,
			},
			Target: agentproto.Target{
				ThreadID:               request.ThreadID,
				TurnID:                 request.TurnID,
				UseActiveTurnIfOmitted: request.TurnID == "",
			},
			Request: agentproto.Request{
				RequestID: request.RequestID,
				Response:  buildUnsupportedServerRequestResponse(request),
			},
		},
	}}
}
