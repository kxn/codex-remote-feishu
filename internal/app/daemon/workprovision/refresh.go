package workprovision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RefreshHome has no model-supplied text. It projects current SQL facts into the
// exact managed blocks. A baseline mismatch preserves human edits. Docx offers
// no cross-editor CAS, so this check is not advertised as a native write lock.
func (s *Service) RefreshHome(ctx context.Context, projectID string) error {
	var operationID string
	if err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		home, err := tx.GetProjectHome(projectID)
		operationID = home.OperationID
		return err
	}); err != nil {
		return err
	}
	state, err := s.load(ctx, operationID)
	if err != nil {
		return err
	}
	if state.home.SyncState == "conflict" {
		return fmt.Errorf("人工编辑冲突尚未明确恢复，未覆盖首页")
	}
	if state.home.DocumentID == "" || state.home.IndexBlockID == "" || state.home.StatusBlockID == "" {
		return fmt.Errorf("project home has no verified managed block IDs")
	}
	platform, err := s.platform(ctx, state.home.Identity)
	if err != nil {
		return err
	}
	actual, err := platform.ReadHomeBlocks(ctx, state.home.DocumentID, state.home.IndexBlockID, state.home.StatusBlockID)
	if err != nil {
		return err
	}
	var pending []workstore.ProvisionStep
	if err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		all, err := tx.ListProvisionSteps(operationID)
		if err != nil {
			return err
		}
		for _, step := range all {
			if strings.HasPrefix(step.StepID, "projection.") && (step.State == work.OperationUnknown || step.State == work.OperationSending) {
				pending = append(pending, step)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, step := range pending {
		var desired HomeIndex
		if err = json.Unmarshal(step.Intent, &desired); err != nil {
			return err
		}
		text, status := renderIndex(desired)
		if actual.Index.Text == text && actual.Status.Text == status {
			if _, err = s.confirmKnown(ctx, state, step, effect{ID: state.home.DocumentID, Data: actual}); err != nil {
				return err
			}
			if err = s.persistIndex(ctx, &state, platform, actual); err != nil {
				return err
			}
			continue
		}
		if actual.Index.Fingerprint == state.home.IndexBaseline && actual.Status.Fingerprint == state.home.StatusBaseline {
			_ = s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) { home.SyncState = "unknown" })
			return ErrUnknown
		}
	}
	if state.home.IndexBaseline == "" || state.home.StatusBaseline == "" {
		return fmt.Errorf("project home lacks an accepted initial baseline")
	}
	if actual.Index.Fingerprint != state.home.IndexBaseline || actual.Status.Fingerprint != state.home.StatusBaseline {
		if err = s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) { home.SyncState = "conflict" }); err != nil {
			return err
		}
		return fmt.Errorf("首页托管段已被人工修改，保留原文并等待明确恢复")
	}
	desired, err := s.index(ctx, state)
	if err != nil {
		return err
	}
	text, status := renderIndex(desired)
	if actual.Index.Text == text && actual.Status.Text == status {
		return nil
	}
	encoded, _ := json.Marshal(desired)
	key := "projection." + stableID("index", string(encoded), actual.Index.Fingerprint, actual.Status.Fingerprint)
	step, writeErr := s.step(ctx, state, key, state.home.Identity, desired, true, func(ctx context.Context, intent json.RawMessage) (effect, error) {
		var index HomeIndex
		if err := json.Unmarshal(intent, &index); err != nil {
			return effect{}, err
		}
		blocks, err := platform.UpdateHomeIndex(ctx, state.home.DocumentID, state.home.IndexBlockID, state.home.StatusBlockID, index, uuidFor(operationID, key))
		return effect{ID: state.home.DocumentID, Data: blocks}, err
	})
	if writeErr != nil {
		if step.State == work.OperationSending || step.State == work.OperationUnknown {
			_ = s.homeUpdate(ctx, &state, func(home *workstore.ProjectHome) { home.SyncState = "unknown" })
		}
		return writeErr
	}
	blocks, err := decodeStep[HomeBlocks](step)
	if err != nil {
		return err
	}
	return s.persistIndex(ctx, &state, platform, blocks)
}
