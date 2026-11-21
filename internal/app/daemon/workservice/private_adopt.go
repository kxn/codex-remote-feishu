package workservice

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
)

type PrivateAdoptInstructionRequest struct {
	Key             string `json:"key"`
	SourceMessageID string `json:"source_message_id"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
}

func (s *Service) PrivateAdoptInstruction(ctx context.Context, token, runRef string, req PrivateAdoptInstructionRequest) (result Result, err error) {
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, _, e := tx.ValidatePrivateAction(workstore.PrivateActionSource{PersonalScopeID: c.Worker.ScopeID, MessageID: req.SourceMessageID, RunRef: c.Attempt.RunRef})
		if e != nil {
			return e
		}
		result, e = s.privateMutate(tx, scope, "private.adopt-instruction", req.Key, scope.ID, req, func() (string, int64, error) {
			adopted, _, e := tx.AdoptPrivateInstruction(workstore.AdoptPrivateInstructionRequest{Key: req.Key, Source: workstore.PrivateActionSource{PersonalScopeID: scope.ID, MessageID: req.SourceMessageID, RunRef: c.Attempt.RunRef}, Title: req.Title, Summary: req.Summary, Now: s.now()})
			return adopted.TaskID, 1, e
		})
		return e
	})
	return
}
