package workprovision

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Read-only reconciliation remains available after stopping. New establishment
// effects need a fresh lifecycle check immediately before their durable intent.
func (s *Service) allowEstablishmentEffect(tx *workstore.Tx, state snapshot) error {
	project, err := tx.GetProject(state.project.ID)
	if err != nil {
		return err
	}
	switch project.State {
	case work.ProjectPausing, work.ProjectPaused, work.ProjectArchiving, work.ProjectArchived:
		return ErrCancelled
	}
	return nil
}
