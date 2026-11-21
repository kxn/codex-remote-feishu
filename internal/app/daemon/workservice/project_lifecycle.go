package workservice

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ProjectInspectRequest struct {
	ProjectID string `json:"project_id"`
}
type ProjectLifecycleRequest struct {
	Key              string `json:"key"`
	ProjectID        string `json:"project_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	SourceMessageID  string `json:"source_message_id"`
}
type ProjectView struct {
	Project work.Project         `json:"project"`
	Members []work.ProjectMember `json:"members"`
	Gates   []work.PauseGate     `json:"gates"`
}

func currentProject(tx *workstore.Tx, c Caller, id string, leader bool) (work.Project, error) {
	project, err := tx.GetProject(id)
	if err != nil {
		return project, err
	}
	if project.ScopeID != c.Worker.ScopeID || (leader && project.LeaderRoleID != c.Worker.RoleID) {
		return project, workauth.ErrUnauthorized
	}
	return project, nil
}
func (s *Service) ProjectInspect(ctx context.Context, token, run string, req ProjectInspectRequest) (ProjectView, error) {
	var view ProjectView
	err := s.withLifecycleWorker(ctx, token, run, "", "", true, func(tx *workstore.Tx, c Caller) error {
		var err error
		if view.Project, err = currentProject(tx, c, req.ProjectID, false); err != nil {
			return err
		}
		if view.Members, err = tx.ListProjectMembers(req.ProjectID); err != nil {
			return err
		}
		view.Gates, err = tx.ListPauseGates(c.Worker.ScopeID)
		return err
	})
	return view, err
}
func projectGate(tx *workstore.Tx, project work.Project, closed bool) error {
	id := stableID("gate", project.ScopeID, "project", project.ID)
	gate := work.PauseGate{ID: id, ScopeID: project.ScopeID, Source: work.GateProject, SourceID: project.ID, State: work.GateOpen, Revision: 1}
	expected := int64(0)
	old, err := tx.GetPauseGate(id)
	if err == nil {
		gate = old
		expected = old.Revision
		gate.Revision++
	} else if err != workstore.ErrNotFound {
		return err
	}
	if closed {
		gate.State = work.GateClosed
	} else {
		gate.State = work.GateOpen
	}
	return tx.PutPauseGate(gate, expected)
}
func projectQuiescent(tx *workstore.Tx, project work.Project, archive bool) (bool, error) {
	workers, err := tx.ListWorkers(project.ScopeID)
	if err != nil {
		return false, err
	}
	for _, worker := range workers {
		context, err := tx.GetContext(worker.ContextID)
		if err != nil {
			return false, err
		}
		if !archive && context.Purpose == work.PurposeControl {
			continue
		}
		busy, err := tx.HasAnyCarrierOccupancy(workstore.CarrierRegistration{OriginInstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration})
		if err != nil {
			return false, err
		}
		if busy {
			return false, nil
		}
	}
	if archive {
		ops, err := tx.ListOperations(project.ScopeID)
		if err != nil {
			return false, err
		}
		for _, op := range ops {
			if workstore.ResourceReadHasNoExternalWrite(op.Kind) {
				continue
			}
			if strings.HasPrefix(op.Kind, "feishu.") || strings.HasPrefix(op.Kind, "resource.") || op.Kind == "project.provision" || op.Kind == "project.member.provision" {
				if op.State != work.OperationConfirmed && op.State != work.OperationTerminal {
					sealed, err := tx.PublicationWasReanchored(op)
					if err != nil {
						return false, err
					}
					if !sealed {
						sealed, err = tx.ResourceCreationWasReplaced(op)
						if err != nil {
							return false, err
						}
					}
					if !sealed {
						return false, nil
					}
				}
			}
		}
	}
	return true, nil
}
func (s *Service) projectLifecycle(ctx context.Context, token, run, kind string, req ProjectLifecycleRequest) (Result, error) {
	var result Result
	err := s.withLifecycleWorker(ctx, token, run, kind, req.Key, true, func(tx *workstore.Tx, c Caller) error {
		if _, _, err := currentHumanInstruction(tx, c, req.SourceMessageID); err != nil {
			return err
		}
		var err error
		result, err = s.mutate(tx, c, kind, req.Key, req.ProjectID, req, func() (string, int64, error) {
			project, err := currentProject(tx, c, req.ProjectID, true)
			if err != nil {
				return "", 0, err
			}
			if project.Revision != req.ExpectedRevision {
				return "", 0, workstore.ErrConflict
			}
			project, err = applyProjectTransition(tx, project, kind)
			if err != nil {
				return "", 0, err
			}
			return project.ID, project.Revision, nil
		})
		if err != nil {
			return err
		}
		if !result.Replay && (kind == "project.pause" || kind == "project.archive") {
			// Completion acknowledges the durable admission closure only; quiescence is
			// a separate runtime fact, reconciled after this very caller has finished.
			result.Status = "accepted"
			op, err := tx.GetOperation(result.OperationID)
			if err != nil {
				return err
			}
			receipt, err := json.Marshal(result)
			if err != nil {
				return err
			}
			old := op.Revision
			op.Revision++
			op.ExternalReceipt = string(receipt)
			_, _, err = tx.PutOperation(op, old)
			return err
		}
		return nil
	})
	return result, err
}
func (s *Service) ProjectPause(ctx context.Context, t, r string, q ProjectLifecycleRequest) (Result, error) {
	return s.projectLifecycle(ctx, t, r, "project.pause", q)
}
func (s *Service) ProjectResume(ctx context.Context, t, r string, q ProjectLifecycleRequest) (Result, error) {
	return s.projectLifecycle(ctx, t, r, "project.resume", q)
}
func (s *Service) ProjectArchive(ctx context.Context, t, r string, q ProjectLifecycleRequest) (Result, error) {
	return s.projectLifecycle(ctx, t, r, "project.archive", q)
}
func (s *Service) ProjectReopen(ctx context.Context, t, r string, q ProjectLifecycleRequest) (Result, error) {
	return s.projectLifecycle(ctx, t, r, "project.reopen", q)
}

// ReconcileProjectLifecycle is a daemon-only local projection. It neither stops
// workers nor clears occupancy: only durable runtime quiescence permits settling.
func (s *Service) ReconcileProjectLifecycle(ctx context.Context, projectID string) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		project, err := tx.GetProject(projectID)
		if err != nil {
			return err
		}
		if project.State != work.ProjectPausing && project.State != work.ProjectArchiving {
			return nil
		}
		quiet, err := projectQuiescent(tx, project, project.State == work.ProjectArchiving)
		if err != nil {
			return err
		}
		if !quiet {
			return nil
		}
		old := project.Revision
		project.Revision++
		if project.State == work.ProjectPausing {
			project.State = work.ProjectPaused
		} else {
			project.State = work.ProjectArchived
		}
		return tx.PutProject(project, old)
	})
}
