package orchestrator

import (
	"log"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const codexReviewFullAccessAutoResponse = "codex_review_full_access"

func (s *Service) maybeAutoApproveCodexReviewRequest(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord) ([]eventcontract.Event, bool) {
	if !s.codexReviewRequestMayAutoApprove(surface, request) || activePendingRequest(surface) != nil {
		return nil, false
	}

	response := codexReviewAutoApprovalResponse(request)
	if response == nil {
		return nil, false
	}
	if request.LocalMeta == nil {
		request.LocalMeta = map[string]string{}
	}
	request.LocalMeta["autoResponse"] = codexReviewFullAccessAutoResponse
	log.Printf(
		"codex review request auto-approved: surface=%s instance=%s thread=%s turn=%s request=%s semantic=%s access=%s",
		strings.TrimSpace(surface.SurfaceSessionID),
		strings.TrimSpace(request.InstanceID),
		strings.TrimSpace(request.ThreadID),
		strings.TrimSpace(request.TurnID),
		strings.TrimSpace(request.RequestID),
		requestPromptSemanticKind(request),
		agentproto.AccessModeFullAccess,
	)
	enqueuePendingRequest(surface, request)
	events := s.dispatchRequestResponse(surface, request, control.Action{}, response, "")
	for _, event := range events {
		if event.Kind == eventcontract.KindAgentCommand && event.Command != nil {
			return []eventcontract.Event{event}, true
		}
	}
	return nil, false
}

func (s *Service) codexReviewRequestMayAutoApprove(surface *state.SurfaceConsoleRecord, request *state.RequestPromptRecord) bool {
	if surface == nil || request == nil || requestPromptBackend(request) != agentproto.BackendCodex {
		return false
	}
	session := s.validReviewSession(surface)
	if session == nil || session.ExecutorKind != state.ReviewExecutorCodexNative || agentproto.NormalizeBackend(session.Backend) != agentproto.BackendCodex {
		return false
	}
	if agentproto.NormalizeAccessMode(session.FrozenAccessMode) != agentproto.AccessModeFullAccess {
		return false
	}
	if strings.TrimSpace(request.InstanceID) == "" || strings.TrimSpace(request.InstanceID) != strings.TrimSpace(surface.AttachedInstanceID) {
		return false
	}
	if strings.TrimSpace(request.ThreadID) == "" || strings.TrimSpace(request.ThreadID) != strings.TrimSpace(session.ReviewThreadID) {
		return false
	}
	if strings.TrimSpace(request.TurnID) == "" || strings.TrimSpace(request.TurnID) != strings.TrimSpace(session.ActiveTurnID) {
		return false
	}

	switch requestPromptSemanticKind(request) {
	case control.RequestSemanticApprovalCommand, control.RequestSemanticApprovalFileChange, control.RequestSemanticApprovalNetwork:
		return requestHasOption(request, "accept")
	case control.RequestSemanticPermissionsRequestApproval:
		return request.Prompt != nil && request.Prompt.Permissions != nil && len(promptPermissionsList(request.Prompt, nil)) != 0
	case control.RequestSemanticApproval:
		switch strings.TrimSpace(request.LocalMeta["requestMethod"]) {
		case "execCommandApproval", "applyPatchApproval":
			return requestHasOption(request, "accept")
		}
	}
	return false
}

func codexReviewAutoApprovalResponse(request *state.RequestPromptRecord) map[string]any {
	if request == nil {
		return nil
	}
	if requestPromptSemanticKind(request) == control.RequestSemanticPermissionsRequestApproval {
		response, complete, _ := buildPermissionsRequestResponse(request, control.Action{
			Request: &control.ActionRequestResponse{
				RequestID:       request.RequestID,
				RequestType:     request.RequestType,
				RequestOptionID: "accept",
			},
		})
		if complete {
			return response
		}
		return nil
	}
	return map[string]any{
		"type":     "approval",
		"decision": "accept",
	}
}
