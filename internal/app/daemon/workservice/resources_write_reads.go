package workservice

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type ResourceListPage struct {
	Items     []work.Resource `json:"items"`
	NextAfter string          `json:"next_after,omitempty"`
}

func (s *Service) ResourceList(ctx context.Context, token, run string, req ResourceListRequest) (ResourceListPage, error) {
	var result ResourceListPage
	limit := req.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "limit must be1..100"}
	}
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap {
			return workstore.ErrScopeDenied
		}
		resources, err := tx.ListResources(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			if resource.ID <= req.After || req.DirectoriesOnly && !resourceDirectory(resource) {
				continue
			}
			if len(result.Items) == limit {
				result.NextAfter = result.Items[len(result.Items)-1].ID
				break
			}
			result.Items = append(result.Items, resource)
		}
		return nil
	})
	return result, err
}

// The resolver is a trusted local binding lookup. All platform reads occur after
// the SQL authorization transaction, using that frozen exact caller identity.
func (s *Service) resourceReader(ctx context.Context, token, run, id string) (work.Resource, ResourceWriteConnector, error) {
	var resource work.Resource
	var reader ResourceWriteConnector
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap {
			return workstore.ErrScopeDenied
		}
		resource, err = tx.GetResource(c.Worker.ScopeID, id)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
		if err != nil {
			return err
		}
		connector, err := s.resolveResourceConnector(ctx, binding)
		if err != nil {
			return err
		}
		if !resourceConnectorAllowed(scope, resource, connector) {
			return workstore.ErrScopeDenied
		}
		var ok bool
		reader, ok = connector.(ResourceWriteConnector)
		if !ok {
			return fmt.Errorf("registered connection does not provide document and sheet editing")
		}
		return nil
	})
	return resource, reader, err
}
func (s *Service) ResourceInspectDocument(ctx context.Context, token, run string, req InspectResourceDocumentRequest) (ResourceDocumentInspection, error) {
	resource, reader, err := s.resourceReader(ctx, token, run, req.ResourceID)
	if err != nil {
		return ResourceDocumentInspection{}, err
	}
	return reader.InspectResourceDocument(ctx, resource, req)
}
func (s *Service) ResourceReadSheet(ctx context.Context, token, run string, req ReadResourceSheetRequest) (ResourceSheetInspection, error) {
	resource, reader, err := s.resourceReader(ctx, token, run, req.ResourceID)
	if err != nil {
		return ResourceSheetInspection{}, err
	}
	return reader.ReadResourceSheet(ctx, resource, req)
}
