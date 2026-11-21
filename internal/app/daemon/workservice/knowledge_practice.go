package workservice

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PracticeReadRequest struct {
	Owner   work.KnowledgeOwner `json:"owner"`
	ID      string              `json:"id"`
	Version string              `json:"version"`
	Offset  int                 `json:"offset,omitempty"`
	Limit   int                 `json:"limit,omitempty"`
}
type PracticeView struct {
	Definition work.PracticeDefinition `json:"definition"`
	Content    ContentPage             `json:"content"`
}
type PracticeAdoptRequest struct {
	Key        string              `json:"key"`
	Owner      work.KnowledgeOwner `json:"owner"`
	ID         string              `json:"id"`
	Version    string              `json:"version"`
	TaskID     string              `json:"task_id,omitempty"`
	Exceptions []string            `json:"exceptions,omitempty"`
}

func (s *Service) PracticeDiscover(ctx context.Context, token, runRef string, req KnowledgeListRequest) (KnowledgePage[work.PracticeDefinition], error) {
	var result KnowledgePage[work.PracticeDefinition]
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		items, err := tx.ListPracticesFor(callerRead(c))
		if err != nil {
			return err
		}
		result, err = knowledgePage(items, req, func(v work.PracticeDefinition) string {
			return fmt.Sprintf("%s/%s/%s/%s/%s", v.Owner.Kind, v.Owner.TeamID, v.Owner.RoleID, v.ID, v.Version)
		})
		return err
	})
	return result, err
}
func (s *Service) PracticeRead(ctx context.Context, token, runRef string, req PracticeReadRequest) (PracticeView, error) {
	var result PracticeView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		definition, body, err := tx.GetPracticeFor(callerRead(c), req.Owner, work.PracticeRef{ID: req.ID, Version: req.Version})
		if err != nil {
			return err
		}
		page, err := knowledgeContentPage(body, req.Offset, req.Limit)
		if err != nil {
			return err
		}
		result = PracticeView{Definition: definition, Content: page}
		return nil
	})
	return result, err
}
func (s *Service) PracticeAdopt(ctx context.Context, token, runRef string, req PracticeAdoptRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		id := stableID("practice-adoption", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "practice.adopt", req.Key, id, req, func() (string, int64, error) {
			if _, _, err := tx.GetPracticeFor(callerRead(c), req.Owner, work.PracticeRef{ID: req.ID, Version: req.Version}); err != nil {
				return "", 0, err
			}
			if req.TaskID != "" {
				task, err := tx.GetTask(req.TaskID)
				if err != nil {
					return "", 0, err
				}
				if err = mayManageTask(tx, c, task); err != nil {
					return "", 0, err
				}
			}
			adoption := work.PracticeAdoption{ID: id, ScopeID: c.Worker.ScopeID, TaskID: req.TaskID, Owner: req.Owner, Practice: work.PracticeRef{ID: req.ID, Version: req.Version}, Exceptions: req.Exceptions, AdoptedBy: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Source: callerSource(c), Revision: 1, AdoptedAt: s.now()}
			if err := tx.PutPracticeAdoption(adoption, 0); err != nil {
				return "", 0, err
			}
			return id, 1, nil
		})
		return err
	})
	return result, err
}
