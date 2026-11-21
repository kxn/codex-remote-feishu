package workservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

func (s *Service) projectExecutionChoice(tx *workstore.Tx, claims workauth.Claims, req *CreateProjectRequest) ([]byte, error) {
	if claims.Purpose == "bootstrap" {
		// This token is only issued to an authenticated human setup submission.
		if len(req.LocationIDs) == 1 && req.DefaultLocationID == "" {
			req.DefaultLocationID = req.LocationIDs[0]
		}
		if len(req.LocationIDs) > 0 && !slices.Contains(req.LocationIDs, req.DefaultLocationID) {
			return nil, fmt.Errorf("choose a default execution directory")
		}
		return json.Marshal(struct {
			Locations   []string `json:"locations"`
			Default     string   `json:"default"`
			PrincipalID string   `json:"principal_id"`
		}{req.LocationIDs, req.DefaultLocationID, req.PrincipalID})
	}
	selection, err := tx.GetExecutionPreference(claims.ScopeID)
	if errors.Is(err, workstore.ErrNotFound) {
		if len(req.LocationIDs) > 0 || req.DefaultLocationID != "" {
			return nil, fmt.Errorf("existing code resources must first be selected by the human")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if selection.SelectedBy.ID != req.PrincipalID {
		return nil, workauth.ErrUnauthorized
	}
	if len(req.LocationIDs) > 0 && !slices.Equal(req.LocationIDs, selection.SelectedLocationIDs) {
		return nil, fmt.Errorf("model cannot replace the human's selected resources")
	}
	if req.DefaultLocationID != "" && req.DefaultLocationID != selection.DefaultLocationID {
		return nil, fmt.Errorf("model cannot replace the selected default location")
	}
	req.LocationIDs = append([]string{}, selection.SelectedLocationIDs...)
	req.DefaultLocationID = selection.DefaultLocationID
	content, err := tx.GetContent(claims.ScopeID, selection.SourceRef)
	if err != nil {
		return nil, err
	}
	return content.Data, nil
}
