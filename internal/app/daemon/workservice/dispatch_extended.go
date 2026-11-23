package workservice

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

// DTO discovery is metadata for contract tests/help integration, never state.
func extendedCommandDTOs() map[string]any {
	dtos := map[string]any{
		"memory.list": KnowledgeListRequest{}, "memory.read": KnowledgeObjectRequest{}, "memory.write": MemoryWriteRequest{}, "memory.invalidate": MemoryInvalidateRequest{},
		"practice.discover": KnowledgeListRequest{}, "practice.read": PracticeReadRequest{}, "practice.adopt": PracticeAdoptRequest{}, "practice.publish": PracticePublishRequest{},
		"resource.register": RegisterResourceRequest{}, "resource.capture": CaptureResourceRequest{}, "resource.read-version": ReadResourceVersionRequest{}, "resource.reconcile": ReconcileResourceRequest{}, "delivery.publish": PublishDeliveryRequest{},
		"decision.list": KnowledgeListRequest{}, "decision.mine": KnowledgeListRequest{}, "decision.respond": DecisionRespondRequest{}, "agreement.list": KnowledgeListRequest{}, "agreement.apply": AgreementApplyRequest{},
	}
	dtos["member.workspace.inspect"] = KnowledgeListRequest{}
	dtos["commands"] = struct{}{}
	dtos["publication.grant"] = GrantPublicationRequest{}
	dtos["publication.revoke"] = RevokePublicationRequest{}
	dtos["media.publish"] = PublishPrivateMediaRequest{}
	dtos["decision.request"] = RequestDecisionRequest{}
	dtos["resource.file.save"] = SaveResourceFileRequest{}
	dtos["session.goal"] = SessionGoalRequest{}
	dtos["control.context"] = struct{}{}
	dtos["control.resume-current"] = ControlResumeCurrentRequest{}
	for name, dto := range resourceWriteCommandDTOs() {
		dtos[name] = dto
	}
	for name, dto := range privateCommandDTOs() {
		dtos[name] = dto
	}
	for name, dto := range lifecycleCommandDTOs() {
		dtos[name] = dto
	}
	return dtos
}
func executeDTO[Input, Output any](ctx context.Context, token string, req workcmd.DispatchRequest, execute func(context.Context, string, string, Input) (Output, error)) (any, bool, error) {
	var input Input
	if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
		return nil, true, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
	}
	result, err := execute(ctx, token, req.RunRef, input)
	return result, true, err
}
func (s *Service) executeExtended(ctx context.Context, token string, req workcmd.DispatchRequest) (any, bool, error) {
	if result, handled, err := s.executeResourceWrites(ctx, token, req); handled {
		return result, true, err
	}
	if result, handled, err := s.executeExtendedPrivate(ctx, token, req); handled {
		return result, true, err
	}
	switch req.Name {
	case "control.context":
		return executeDTO(ctx, token, req, func(ctx context.Context, token, run string, _ struct{}) (ControlContextView, error) {
			return s.ControlContext(ctx, token, run)
		})
	case "control.resume-current":
		return executeDTO(ctx, token, req, s.ControlResumeCurrent)
	case "session.goal":
		return executeDTO(ctx, token, req, s.SessionGoal)
	case "decision.request":
		return executeDTO(ctx, token, req, s.RequestDecision)

	case "member.workspace.inspect":
		return executeDTO(ctx, token, req, s.MemberWorkspaceInspect)
	case "memory.list":
		return executeDTO(ctx, token, req, s.MemoryList)
	case "memory.read":
		return executeDTO(ctx, token, req, s.MemoryRead)
	case "memory.write":
		return executeDTO(ctx, token, req, s.MemoryWrite)
	case "memory.invalidate":
		return executeDTO(ctx, token, req, s.MemoryInvalidate)
	case "practice.discover":
		return executeDTO(ctx, token, req, s.PracticeDiscover)
	case "practice.read":
		return executeDTO(ctx, token, req, s.PracticeRead)
	case "practice.adopt":
		return executeDTO(ctx, token, req, s.PracticeAdopt)
	case "practice.publish":
		return executeDTO(ctx, token, req, s.PracticePublish)
	case "resource.register":
		return executeDTO(ctx, token, req, s.ResourceRegister)
	case "resource.file.save":
		return executeDTO(ctx, token, req, s.ResourceSaveFile)
	case "resource.capture":
		return executeDTO(ctx, token, req, s.ResourceCapture)
	case "resource.read-version":
		return executeDTO(ctx, token, req, s.ResourceReadVersion)
	case "resource.reconcile":
		return executeDTO(ctx, token, req, s.ResourceReconcile)
	case "delivery.publish":
		return executeDTO(ctx, token, req, s.PublishDelivery)
	case "decision.list":
		return executeDTO(ctx, token, req, s.DecisionList)
	case "decision.mine":
		return executeDTO(ctx, token, req, s.DecisionMine)
	case "decision.respond":
		return executeDTO(ctx, token, req, s.DecisionRespond)
	case "agreement.list":
		return executeDTO(ctx, token, req, s.AgreementList)
	case "agreement.apply":
		return executeDTO(ctx, token, req, s.AgreementApply)
	default:
		return s.executeLifecycle(ctx, token, req)
	}
}
