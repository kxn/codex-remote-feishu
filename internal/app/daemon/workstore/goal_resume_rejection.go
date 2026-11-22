package workstore

import (
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// Only unclaimed resume inputs can be cancelled here. An attempt that may have
// crossed native dispatch keeps its uncertainty and normal recovery ownership.
func (t *Tx) CancelStaleSessionGoalResumes(now time.Time) ([]SessionGoalIntent, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT i.payload FROM session_goal_intents i JOIN inbox n ON n.id=json_extract(i.payload,'$.input_id') WHERE json_extract(i.payload,'$.goal.expected') IS NOT NULL AND json_extract(i.payload,'$.rejection_reason') IS NULL AND json_extract(n.payload,'$.state')='pending' ORDER BY COALESCE(json_extract(i.payload,'$.last_checked_at'),''),i.id LIMIT 64`)
	if err != nil {
		return nil, err
	}
	list := []SessionGoalIntent{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var v SessionGoalIntent
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	rejected := []SessionGoalIntent{}
	for _, intent := range list {
		carrier, e := t.GetCarrier(intent.Goal.Expected.Incarnation)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return nil, e
		}
		worker := work.WorkerSession{}
		if e == nil {
			worker, e = t.GetWorker(carrier.Owner.WorkerSessionID)
		}
		if e != nil && !errors.Is(e, ErrNotFound) {
			return nil, e
		}
		if e == nil {
			e = t.ValidateGoalPrecondition(intent.Goal.Expected, worker, carrier, worker.NativeSessionID)
		}
		if e == nil {
			// Valid pending requests must move behind unvisited rows too: a
			// paused batch cannot starve later stale requests indefinitely.
			intent.LastCheckedAt = now
			raw, err := json.Marshal(intent)
			if err != nil {
				return nil, err
			}
			if _, err = t.tx.ExecContext(t.ctx, "UPDATE session_goal_intents SET payload=? WHERE id=?", string(raw), intent.ID); err != nil {
				return nil, err
			}
			continue
		}
		if !errors.Is(e, ErrNotFound) && !errors.Is(e, ErrGoalPreconditionStale) && !errors.Is(e, ErrScopeDenied) {
			return nil, e
		}
		input, err := t.GetInbox(intent.InputID)
		if err != nil {
			return nil, err
		}
		if input.State != work.InboxPending || input.ClaimedAttemptID != "" {
			continue
		}
		before := input.Revision
		input.Revision++
		input.State = work.InboxCancelled
		if err = t.PutInbox(input, before); err != nil {
			return nil, err
		}
		intent.RejectedAt = now
		intent.RejectionReason = "会话或目标已改变，这次恢复没有执行。请刷新目标页面后重新选择。"
		raw, err := json.Marshal(intent)
		if err != nil {
			return nil, err
		}
		if _, err = t.tx.ExecContext(t.ctx, "UPDATE session_goal_intents SET payload=? WHERE id=?", string(raw), intent.ID); err != nil {
			return nil, err
		}
		rejected = append(rejected, intent)
	}
	return rejected, nil
}
