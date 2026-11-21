package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func privateCommandDTOs() map[string]any {
	return map[string]any{"private.context": struct{}{}, "private.project-context": struct{}{}, "private.project-content": PrivateProjectReadContentRequest{}, "private.navigate": PrivateNavigateRequest{}, "private.adopt-instruction": PrivateAdoptInstructionRequest{}}
}
func (s *Service) executeExtendedPrivate(ctx context.Context, token string, req workcmd.DispatchRequest) (any, bool, error) {
	switch req.Name {
	case "private.context":
		return executeDTO(ctx, token, req, func(ctx context.Context, token, run string, _ struct{}) (PrivateNavigationView, error) {
			return s.PrivateView(ctx, token, run)
		})
	case "private.project-context":
		return executeDTO(ctx, token, req, func(ctx context.Context, token, run string, _ struct{}) (PrivateProjectContextView, error) {
			return s.PrivateProjectContext(ctx, token, run)
		})
	case "private.project-content":
		return executeDTO(ctx, token, req, s.PrivateProjectReadContent)
	case "private.navigate":
		return executeDTO(ctx, token, req, s.PrivateNavigate)
	case "private.adopt-instruction":
		return executeDTO(ctx, token, req, s.PrivateAdoptInstruction)
	}
	return nil, false, nil
}
