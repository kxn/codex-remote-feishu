package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) markStage(ctx context.Context, state *snapshot, id string, cause error) error {
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(state.operation.ID)
		if err != nil {
			return err
		}
		if cancellationRequested(operation) {
			return ErrCancelled
		}
		if operation.State == work.OperationPrepared || operation.State == work.OperationRetryable {
			operation.State = work.OperationSending
		} else if operation.State == work.OperationUnknown {
			operation.State = work.OperationRetryable
			operation.Revision++
			if _, _, err = tx.PutOperation(operation, operation.Revision-1); err != nil {
				return err
			}
			operation.State = work.OperationSending
		}
		for i := range operation.Steps {
			if operation.Steps[i].ID == id {
				if cause == nil {
					operation.Steps[i].State = work.OperationConfirmed
					operation.Steps[i].Receipt = "provision-steps:" + id
				} else {
					operation.Steps[i].State = work.OperationUnknown
				}
			}
		}
		previous := operation.Revision
		operation.Revision++
		operation.UpdatedAt = s.now()
		_, _, err = tx.PutOperation(operation, previous)
		state.operation = operation
		return err
	})
}
func (s *Service) finish(ctx context.Context, state snapshot, blockers []string) (Report, error) {
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		operation, err := tx.GetOperation(state.operation.ID)
		if err != nil {
			return err
		}
		all := true
		for _, step := range operation.Steps {
			if step.State != work.OperationConfirmed {
				all = false
			}
		}
		project, err := tx.GetProject(state.project.ID)
		if err != nil {
			return err
		}
		members, err := tx.ListProjectMembers(project.ID)
		if err != nil {
			return err
		}
		ready := true
		for _, member := range members {
			if member.State != work.ProjectMemberReady {
				ready = false
			}
		}
		previous := project.Revision
		project.Revision++
		if all && ready && len(blockers) == 0 {
			project.State = work.ProjectActive
		} else {
			project.State = work.ProjectDegraded
		}
		if err = tx.PutProject(project, previous); err != nil {
			return err
		}
		previous = operation.Revision
		operation.Revision++
		operation.UpdatedAt = s.now()
		if all {
			operation.State = work.OperationConfirmed
			receipt, _ := json.Marshal(map[string]any{"project_id": project.ID, "state": project.State, "steps": "provision_steps", "blockers": blockers})
			operation.ExternalReceipt = string(receipt)
		} else {
			operation.State = work.OperationUnknown
			operation.LastError = fmt.Sprint(blockers)
		}
		_, _, err = tx.PutOperation(operation, previous)
		return err
	})
	if err != nil {
		return Report{OperationID: state.operation.ID, ProjectID: state.project.ID, Blockers: blockers}, err
	}
	return s.report(ctx, state, blockers)
}
func (s *Service) prepareExecution(ctx context.Context, state *snapshot) error {
	// Existing directories are usable only through the persisted human choice.
	// No preference and no locations means the preparer creates isolated scratch.
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		pref, err := tx.GetExecutionPreference(state.project.ScopeID)
		if errors.Is(err, workstore.ErrNotFound) && len(state.request.LocationIDs) == 0 && state.request.DefaultLocationID == "" {
			return nil
		}
		if err != nil {
			return fmt.Errorf("execution requires the saved human location selection: %w", err)
		}
		if !slices.Equal(pref.SelectedLocationIDs, state.request.LocationIDs) || pref.DefaultLocationID != state.request.DefaultLocationID || pref.SelectedBy != state.operation.Actor {
			return fmt.Errorf("provisioning location selection differs from the saved human choice")
		}
		return s.current(tx, *state)
	}); err != nil {
		return err
	}

	_, err := s.step(ctx, *state, "execution.prepare", state.home.Identity, state.request.LocationIDs, false, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var ids []string
		if err := json.Unmarshal(intent, &ids); err != nil {
			return effect{}, err
		}
		err := s.execution.PrepareProject(ctx, state.project, ids)
		return effect{Data: map[string]any{"selected_location_ids": ids, "prepared": err == nil}}, err
	})
	return err
}
func (s *Service) initialInput(ctx context.Context, state *snapshot) error {
	_, err := s.step(ctx, *state, "initial_input.adopt", state.home.Identity, map[string]string{"source_message_id": state.request.SourceMessageID}, false, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var request map[string]string
		if err := json.Unmarshal(intent, &request); err != nil {
			return effect{}, err
		}
		var message work.Message
		created := false
		err := s.store.Within(ctx, func(tx *workstore.Tx) error {
			if err := s.current(tx, *state); err != nil {
				return err
			}
			var err error
			message, created, err = tx.AdoptBootstrapInput(workstore.AdoptBootstrapInputRequest{OperationID: state.operation.ID, SourceMessageID: request["source_message_id"]})
			return err
		})
		return effect{ID: message.ID, Data: map[string]any{"message_id": message.ID, "created": created, "derived_from": message.DerivedFrom}}, err
	})
	return err
}
