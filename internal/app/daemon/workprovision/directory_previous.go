package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strconv"
	"strings"
)

// A new selection is not permission to race an older unsettled move. Preserve
// the new choice, but reconcile the old exact destination before another move.
func (s *Service) settlePreviousDirectoryMoves(ctx context.Context, state snapshot, mover HomeDirectoryPlatform, documentID string) error {
	var steps []workstore.ProvisionStep
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		steps, e = tx.ListProvisionSteps(state.operation.ID)
		return e
	})
	if err != nil {
		return err
	}
	for _, step := range steps {
		parts := strings.Split(step.StepID, ".")
		if len(parts) != 3 || parts[0] != "directory" || parts[2] != "move" {
			continue
		}
		generation, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return err
		}
		if generation >= state.home.DirectoryGeneration || generation <= state.home.DirectoryAppliedGeneration {
			continue
		}
		var intent struct {
			DocumentID string          `json:"document_id"`
			Target     DirectoryTarget `json:"target"`
		}
		if err = json.Unmarshal(step.Intent, &intent); err != nil {
			return err
		}
		if intent.DocumentID != documentID {
			return workstore.ErrScopeDenied
		}
		observed, e := s.storeDirectoryMoveObserved(ctx, state.operation.ID, generation)
		if e != nil {
			return e
		}
		if observed {
			continue
		}
		present, err := mover.IsDocumentInDirectory(ctx, documentID, state.home.ContainerKind, state.home.ContainerID, intent.Target)
		if err != nil {
			return err
		}
		if !present {
			return fmt.Errorf("已保存新目录；前一次目录移动尚未核实，不能并行再次移动同一文档")
		}
		if step.State != work.OperationConfirmed {
			if _, err = s.confirmKnown(ctx, state, step, effect{ID: documentID, Data: map[string]any{"document_id": documentID, "observed_target": intent.Target}}); err != nil {
				return err
			}
		}
		if err = s.recordDirectoryMoveObserved(ctx, state, generation, documentID, intent.Target); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) storeDirectoryMoveObserved(ctx context.Context, operationID string, generation int64) (bool, error) {
	var observed bool
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		step, e := tx.GetProvisionStep(operationID, "directory."+strconv.FormatInt(generation, 10)+".destination")
		if e == workstore.ErrNotFound {
			return nil
		}
		if e != nil {
			return e
		}
		observed = step.State == work.OperationConfirmed
		return nil
	})
	return observed, err
}
func (s *Service) recordDirectoryMoveObserved(ctx context.Context, state snapshot, generation int64, id string, target DirectoryTarget) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		key := "directory." + strconv.FormatInt(generation, 10) + ".destination"
		if _, err := tx.GetProvisionStep(state.operation.ID, key); err == nil {
			return nil
		} else if err != workstore.ErrNotFound {
			return err
		}
		raw, _ := json.Marshal(map[string]any{"document_id": id, "actual_target": target, "observed_at": s.now()})
		step := workstore.ProvisionStep{ID: workstore.ProvisionStepID(state.operation.ID, key), ScopeID: state.operation.ScopeID, OperationID: state.operation.ID, StepID: key, Identity: state.home.Identity, Intent: raw, Receipt: raw, ExternalID: id, State: work.OperationPrepared, Revision: 1, UpdatedAt: s.now()}
		if err := tx.PutProvisionStep(step, 0); err != nil {
			return err
		}
		step.State = work.OperationSending
		step.Revision++
		if err := tx.PutProvisionStep(step, 1); err != nil {
			return err
		}
		step.State = work.OperationConfirmed
		step.Revision++
		return tx.PutProvisionStep(step, 2)
	})
}
