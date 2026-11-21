package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
	"net/url"
	"strconv"
	"strings"
)

type ProjectDirectoryRequest struct {
	Key                         string `json:"key"`
	ProjectID                   string `json:"project_id"`
	ExpectedProjectRevision     int64  `json:"expected_project_revision"`
	ExpectedDirectoryGeneration int64  `json:"expected_directory_generation"`
	DocumentContainer           string `json:"document_container"`
	SourceMessageID             string `json:"source_message_id"`
}
type ProjectDirectorySelection struct {
	Container   string         `json:"container"`
	Generation  int64          `json:"generation"`
	Source      work.SourceRef `json:"source"`
	PrincipalID string         `json:"principal_id"`
}

func (s *Service) ProjectSelectDirectory(ctx context.Context, token, run string, req ProjectDirectoryRequest) (Result, error) {
	var result Result
	parsed, e := url.Parse(req.DocumentContainer)
	host := ""
	if parsed != nil {
		host = strings.ToLower(parsed.Hostname())
	}
	if e != nil || parsed == nil || parsed.Scheme != "https" || parsed.User != nil || !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "explicit existing Feishu Drive folder or Wiki parent HTTPS URL required"}
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if !(len(parts) == 3 && parts[0] == "drive" && parts[1] == "folder" || len(parts) == 2 && parts[0] == "wiki" || len(parts) == 3 && parts[0] == "wiki" && parts[1] == "space") {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "choose an existing Drive folder, Wiki node or explicit existing space root"}
	}
	err := s.withManagementWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		var err error
		result, err = s.mutate(tx, c, "project.directory.select", req.Key, req.ProjectID, req, func() (string, int64, error) {
			message, _, err := currentHumanInstruction(tx, c, req.SourceMessageID)
			if err != nil {
				return "", 0, err
			}
			if err = directorySourceContainsURL(tx, message, req.DocumentContainer); err != nil {
				return "", 0, err
			}
			return s.applyProjectDirectorySelection(tx, req, c.Worker.ScopeID, c.Worker.RoleID, message)

		})
		if err == nil && !result.Replay {
			result.Status = "selection_saved"
		}
		return err
	})
	return result, err
}
func (s *Service) applyProjectDirectorySelection(tx *workstore.Tx, req ProjectDirectoryRequest, scopeID, roleID string, message work.Message) (string, int64, error) {
	project, err := tx.GetProject(req.ProjectID)
	if err != nil {
		return "", 0, err
	}
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return "", 0, err
	}
	if scope.Kind != work.ScopeProject || project.ScopeID != scope.ID || project.LeaderRoleID != roleID {
		return "", 0, workstore.ErrScopeDenied
	}
	if project.State == work.ProjectArchiving || project.State == work.ProjectArchived {
		return "", 0, &workcmd.Error{Code: "blocked", Message: "stopped establishment cannot accept a new directory selection"}
	}
	if project.Revision != req.ExpectedProjectRevision {
		return "", 0, workstore.ErrConflict
	}
	ops, err := tx.ListOperations(scope.ID)
	if err != nil {
		return "", 0, err
	}
	var provisioning work.Operation
	for _, op := range ops {
		if op.Kind == "project.provision" && op.TargetID == project.ID {
			if provisioning.ID != "" {
				return "", 0, fmt.Errorf("project has ambiguous establishment authority")
			}
			provisioning = op
		}
	}
	if provisioning.ID == "" || provisioning.State == work.OperationConfirmed || cancellationMarker(provisioning) {
		return "", 0, &workcmd.Error{Code: "blocked", Message: "directory selection is limited to an ongoing, uncancelled project establishment"}
	}
	original, err := tx.GetContent(scope.ID, provisioning.RequestRef)
	if err != nil {
		return "", 0, err
	}
	var creation CreateProjectRequest
	if err = json.Unmarshal(original.Data, &creation); err != nil {
		return "", 0, err
	}
	if creation.PrincipalID != message.Source.ID {
		return "", 0, workstore.ErrScopeDenied
	}
	home, err := tx.GetProjectHome(project.ID)
	if err == workstore.ErrNotFound {
		binding, e := tx.GetRoleBotBinding(project.LeaderRoleID)
		if e != nil {
			return "", 0, e
		}
		home = workstore.ProjectHome{ID: project.ID, ScopeID: scope.ID, OperationID: provisioning.ID, ChannelGeneration: provisioning.TargetGeneration, Identity: binding, SyncState: "pending", Revision: 0}
	} else if err != nil {
		return "", 0, err
	}
	if home.DirectoryGeneration != req.ExpectedDirectoryGeneration {
		return "", 0, workstore.ErrConflict
	}
	selected := ProjectDirectorySelection{Container: req.DocumentContainer, Generation: home.DirectoryGeneration + 1, Source: work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}, PrincipalID: message.Source.ID}
	raw, err := json.Marshal(selected)
	if err != nil {
		return "", 0, err
	}
	content := work.NewContent(stableID("project-directory", project.ID, strconv.FormatInt(selected.Generation, 10)), scope.ID, "application/json", raw, s.now())
	if err = tx.PutContent(content); err != nil {
		return "", 0, err
	}
	previous := home.Revision
	home.Revision++
	home.DirectoryGeneration = selected.Generation
	home.DirectorySelectionRef = content.ID
	home.SyncState = "pending"
	home.UpdatedAt = s.now()
	if err = tx.PutProjectHome(home, previous); err != nil {
		return "", 0, err
	}
	if provisioning.State == work.OperationUnknown {
		previous := provisioning.Revision
		provisioning.State = work.OperationRetryable
		provisioning.Revision++
		provisioning.UpdatedAt = s.now()
		if _, _, err = tx.PutOperation(provisioning, previous); err != nil {
			return "", 0, err
		}
	}
	return project.ID, selected.Generation, nil
}

func cancellationMarker(op work.Operation) bool {
	for _, step := range op.Steps {
		if step.ID == "cancellation_requested" {
			return true
		}
	}
	return false
}
