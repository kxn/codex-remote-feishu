package workstore

import (
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ListPreparationOccupants counts pending, usable prepared workstreams, not all
// historical contexts. Blocked idle carriers may be safely retired by the host
// pool; they must not starve an unrelated project's preparation budget.
func (t *Tx) ListPreparationOccupants() ([]work.WorkerSession, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT w.payload FROM workers w JOIN contexts c ON c.id=json_extract(w.payload,'$.context_id') WHERE json_extract(w.payload,'$.state')='preparing' OR (json_extract(w.payload,'$.state')='ready' AND EXISTS(SELECT 1 FROM inbox i WHERE i.state='pending' AND i.scope_id=json_extract(w.payload,'$.scope_id') AND i.role_id=json_extract(w.payload,'$.role_id') AND (json_extract(i.payload,'$.recipient_context_id')=c.id OR (COALESCE(json_extract(i.payload,'$.recipient_context_id'),'')='' AND COALESCE(json_extract(i.payload,'$.task_id'),'')=COALESCE(json_extract(c.payload,'$.task_id'),'')))))`)
	if err != nil {
		return nil, err
	}
	values := []work.WorkerSession{}
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return nil, err
		}
		var worker work.WorkerSession
		if err = json.Unmarshal([]byte(body), &worker); err != nil {
			rows.Close()
			return nil, err
		}
		values = append(values, worker)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	result := []work.WorkerSession{}
	for _, worker := range values {
		if worker.State == work.WorkerPreparing {
			result = append(result, worker)
			continue
		}
		if _, err = t.GetRuntimeGate(worker.ID); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		context, err := t.GetContext(worker.ContextID)
		if err != nil {
			return nil, err
		}
		gates, err := t.ListPauseGates(worker.ScopeID)
		if err != nil {
			return nil, err
		}
		allowed, err := work.GatesAllowRunForPurpose(worker.ScopeID, context.TaskID, context.ID, context.Purpose, gates)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		if context.TaskID != "" {
			task, err := t.GetTask(context.TaskID)
			if err != nil {
				return nil, err
			}
			if task.State == work.TaskDone || task.State == work.TaskCancelled {
				continue
			}
			dependencies, err := t.ListDependencies(task.ID)
			if err != nil {
				return nil, err
			}
			blocked := false
			for _, dependency := range dependencies {
				if dependency.SatisfiedBy == nil {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
		}
		if worker.LocationID != "" {
			location, err := t.GetLocation(worker.LocationID)
			if err != nil {
				return nil, err
			}
			blocked := false
			for _, key := range append([]string{"cwd:" + location.HostID + ":" + location.Cwd}, location.ResourceKeys...) {
				if err = t.AssertRuntimeResourceAvailable(key); err != nil {
					blocked = true
					break
				}
				var occupied int
				if err = t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM resource_occupancy WHERE resource_key=?", key).Scan(&occupied); err != nil {
					return nil, err
				}
				if occupied > 0 {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
		}
		result = append(result, worker)
	}
	return result, nil
}
