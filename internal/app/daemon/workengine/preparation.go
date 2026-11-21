package workengine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type preparationResult struct {
	Need WorkerNeed
	Err  error
}

func preparationKey(tx *workstore.Tx, need WorkerNeed) (string, work.ContextPurpose, error) {
	contextID := need.ContextID
	purpose := work.PurposeBusiness
	if contextID != "" {
		context, err := tx.GetContext(contextID)
		if err != nil {
			return "", purpose, err
		}
		purpose = context.Purpose
		if context.Workstream == "main" || need.TaskID != "" {
			contextID = ""
		}
	}
	key, _ := json.Marshal([]string{need.ScopeID, need.RoleID, need.TaskID, contextID})
	return string(key), purpose, nil
}
func (e *Engine) startPreparations(ctx context.Context, needs []WorkerNeed, result *TickResult) error {
	if e.config.Provisioner == nil {
		return nil
	}
	e.prepareMu.Lock()
	defer e.prepareMu.Unlock()
	if e.preparationsClosed {
		return nil
	}
	for {
		select {
		case finished := <-e.preparationResults:
			if finished.Err != nil {
				result.Deferred = append(result.Deferred, Deferral{Need: finished.Need, Reason: finished.Err.Error()})
			}
		default:
			goto drained
		}
	}
drained:
	var ordered []WorkerNeed
	err := e.store.Within(ctx, func(tx *workstore.Tx) error {
		cursor, err := tx.GetSchedulingCursor("prepare:" + e.config.SchedulerID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		groups := map[SchedulingGroup][]WorkerNeed{}
		targets := []workstore.SchedulingTarget{}
		for _, need := range needs {
			key := SchedulingGroup{need.ScopeID, need.RoleID}
			if len(groups[key]) == 0 {
				targets = append(targets, workstore.SchedulingTarget{ScopeID: need.ScopeID, RoleID: need.RoleID})
			}
			groups[key] = append(groups[key], need)
		}
		targets = fairTargets(targets, cursor)
		for round := 0; ; round++ {
			any := false
			for _, target := range targets {
				group := groups[SchedulingGroup{target.ScopeID, target.RoleID}]
				if round < len(group) {
					ordered = append(ordered, group[round])
					any = true
				}
			}
			if !any {
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, need := range ordered {
		var key string
		var purpose work.ContextPurpose
		selected := false
		err = e.store.Within(ctx, func(tx *workstore.Tx) error {
			var err error
			key, purpose, err = preparationKey(tx, need)
			if err != nil {
				return err
			}
			if _, exists := e.preparing[key]; exists {
				return nil
			}
			total, err := tx.ActiveSlotCount()
			if err != nil {
				return err
			}
			business, err := tx.ActiveBusinessSlotCount()
			if err != nil {
				return err
			}
			warm := map[string]work.ContextPurpose{}
			for id, entry := range e.preparing {
				warm[id] = entry
			}
			workers, err := tx.ListPreparationOccupants()
			if err != nil {
				return err
			}
			for _, worker := range workers {
				context, err := tx.GetContext(worker.ContextID)
				if err != nil {
					return err
				}
				id, category, err := preparationKey(tx, WorkerNeed{ScopeID: worker.ScopeID, RoleID: worker.RoleID, TaskID: context.TaskID, ContextID: context.ID})
				if err != nil {
					return err
				}
				warm[id] = category
			}
			for _, category := range warm {
				total++
				if category != work.PurposeControl {
					business++
				}
			}
			if _, alreadyWarm := warm[key]; alreadyWarm {
				total--
				if purpose != work.PurposeControl {
					business--
				}
			}
			if total >= e.config.MaxConcurrent || (purpose != work.PurposeControl && business >= e.config.MaxConcurrent-e.config.ControlReservedSlots) {
				return nil
			}
			cursor, err := tx.GetSchedulingCursor("prepare:" + e.config.SchedulerID)
			expected := int64(0)
			if errors.Is(err, workstore.ErrNotFound) {
				cursor = workstore.SchedulingCursor{ID: "prepare:" + e.config.SchedulerID, RoleByScope: map[string]string{}, Revision: 1}
			} else if err != nil {
				return err
			} else {
				expected = cursor.Revision
				cursor.Revision++
			}
			cursor.LastScopeID = need.ScopeID
			if cursor.RoleByScope == nil {
				cursor.RoleByScope = map[string]string{}
			}
			cursor.RoleByScope[need.ScopeID] = need.RoleID
			if err = tx.PutSchedulingCursor(cursor, expected); err != nil {
				return err
			}
			selected = true
			return nil
		})
		if err != nil {
			return err
		}
		if !selected {
			continue
		}
		e.preparing[key] = purpose
		e.prepareWG.Add(1)
		go func(key string, need WorkerNeed) {
			defer e.prepareWG.Done()
			err := e.config.Provisioner.Prepare(ctx, need)
			e.prepareMu.Lock()
			delete(e.preparing, key)
			select {
			case e.preparationResults <- preparationResult{Need: need, Err: err}:
			default:
			}
			e.prepareMu.Unlock()
		}(key, need)
	}
	return nil
}

type SchedulingGroup struct{ ScopeID, RoleID string }

// DrainPreparations fences new host preparation before waiting for actual
// launch calls to return. A canceled wait never asserts that launches stopped.
func (e *Engine) DrainPreparations(ctx context.Context) error {
	e.prepareMu.Lock()
	e.preparationsClosed = true
	e.prepareMu.Unlock()
	done := make(chan struct{})
	go func() { e.prepareWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
