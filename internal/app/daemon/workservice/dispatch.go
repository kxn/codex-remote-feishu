package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func (s *Service) Execute(ctx context.Context, token string, req workcmd.DispatchRequest) (any, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	switch req.Name {
	case "bootstrap.location.pick":
		result, _, err := executeDTO(ctx, token, req, s.BootstrapPickLocation)
		return result, err
	case "bootstrap.team.resolve":
		result, _, err := executeDTO(ctx, token, req, s.BootstrapResolveTeam)
		return result, err
	case "execution.locations":
		var input struct{}
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.ExecutionLocations(ctx, token, req.RunRef)
	case "execution.location.use":
		var input ExecutionLocationRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.UseExecutionLocation(ctx, token, req.RunRef, input)
	case "control.consume":
		var payload ControlConsumeRequest
		if err := workcmd.DecodePayload(req.Payload, &payload); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.ConsumeControlInput(ctx, token, req.RunRef, payload)
	case "context":
		var input struct{}
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.Context(ctx, token, req.RunRef)
	case "task.assign":
		var input AssignTaskRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.AssignTask(ctx, token, req.RunRef, input)
	case "task.revise":
		var input ReviseTaskRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.ReviseTask(ctx, token, req.RunRef, input)
	case "message.send":
		var input SendMessageRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.SendMessage(ctx, token, req.RunRef, input)
	case "outcome.report":
		var input ReportOutcomeRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.ReportOutcome(ctx, token, req.RunRef, input)
	case "project.create":
		var input CreateProjectRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.CreateProjectInRun(ctx, token, req.RunRef, input)
	case "content.read":
		var input ContentReadRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.ReadContent(ctx, token, req.RunRef, input)
	case "bootstrap.context":
		var input struct{}
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.BootstrapContext(ctx, token, req.RunRef)
	case "message.publish":
		var input PublishMessageRequest
		if err := workcmd.DecodePayload(req.Payload, &input); err != nil {
			return nil, &workcmd.Error{Code: "invalid_argument", Message: err.Error()}
		}
		return s.PublishMessage(ctx, token, req.RunRef, input)
	default:
		if result, handled, err := s.executeExtended(ctx, token, req); handled {
			return result, err
		}
		return nil, &workcmd.Error{Code: "unknown_command", Message: "unsupported team command"}
	}
}
