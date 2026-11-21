package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func resourceWriteCommandDTOs() map[string]any {
	return map[string]any{
		"project.directory.select":        ProjectDirectoryRequest{},
		"resource.comments.read":          ReadResourceCommentsRequest{},
		"resource.write.observe":          ObserveResourceWriteRequest{},
		"resource.document.share-project": ShareProjectDocumentRequest{},
		"resource.list":                   ResourceListRequest{}, "resource.document.create": CreateResourceDocumentRequest{}, "resource.document.inspect": InspectResourceDocumentRequest{}, "resource.document.append": AppendResourceDocumentRequest{}, "resource.document.update": UpdateResourceDocumentRequest{}, "resource.sheet.read": ReadResourceSheetRequest{}, "resource.sheet.write": WriteResourceSheetRequest{},
	}
}
func (s *Service) executeResourceWrites(ctx context.Context, token string, req workcmd.DispatchRequest) (any, bool, error) {
	switch req.Name {
	case "project.directory.select":
		return executeDTO(ctx, token, req, s.ProjectSelectDirectory)
	case "resource.comments.read":
		return executeDTO(ctx, token, req, s.ResourceReadComments)
	case "resource.write.observe":
		return executeDTO(ctx, token, req, s.ResourceObserveWrite)
	case "resource.list":
		return executeDTO(ctx, token, req, s.ResourceList)
	case "resource.document.create":
		return executeDTO(ctx, token, req, s.ResourceCreateDocument)
	case "resource.document.share-project":
		return executeDTO(ctx, token, req, s.ShareProjectDocument)
	case "resource.document.inspect":
		return executeDTO(ctx, token, req, s.ResourceInspectDocument)
	case "resource.document.append":
		return executeDTO(ctx, token, req, s.ResourceAppendDocument)
	case "resource.document.update":
		return executeDTO(ctx, token, req, s.ResourceUpdateDocument)
	case "resource.sheet.read":
		return executeDTO(ctx, token, req, s.ResourceReadSheet)
	case "resource.sheet.write":
		return executeDTO(ctx, token, req, s.ResourceWriteSheet)
	}
	return nil, false, nil
}
