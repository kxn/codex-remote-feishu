package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type SchedulingTarget struct {
	ScopeID string
	RoleID  string
}
type SchedulingCursor struct {
	ID          string            `json:"id"`
	LastScopeID string            `json:"last_scope_id"`
	RoleByScope map[string]string `json:"role_by_scope"`
	Revision    int64             `json:"revision"`
}

func (v SchedulingCursor) Validate() error {
	if v.ID == "" || v.Revision < 1 {
		return fmt.Errorf("scheduler cursor requires id and positive revision")
	}
	return nil
}
func (t *Tx) GetSchedulingCursor(id string) (SchedulingCursor, error) {
	var v SchedulingCursor
	err := t.read("scheduler_cursors", id, &v)
	return v, err
}
func (t *Tx) PutSchedulingCursor(v SchedulingCursor, expected int64) error {
	return t.record("scheduler_cursors", v.ID, v.Revision, expected, v)
}
func (t *Tx) ActiveSlotCount() (int, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, "SELECT (SELECT count(*) FROM carrier_occupancy)+(SELECT count(*) FROM native_maintenance WHERE state IN ('sending','unknown'))").Scan(&count)
	return count, err
}
func (t *Tx) ListSchedulingTargets() ([]SchedulingTarget, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT DISTINCT scope_id,role_id FROM inbox WHERE state='pending' ORDER BY scope_id,role_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []SchedulingTarget{}
	for rows.Next() {
		var v SchedulingTarget
		if err = rows.Scan(&v.ScopeID, &v.RoleID); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (t *Tx) ListPendingInputs(scopeID, roleID string) ([]work.Inbox, error) {
	return listRecords[work.Inbox](t, "inbox", " WHERE scope_id=? AND role_id=? AND state='pending' AND coalesce(json_extract(payload,'$.shell_operation_id'),'')=''", scopeID, roleID)
}
func (t *Tx) ListPendingDispatchOperations() ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", " WHERE kind='native.dispatch' AND state IN ('prepared','sending','unknown','retryable')")
}

func (t *Tx) WorkerHasOccupancy(workerID string) (bool, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM carrier_occupancy c JOIN attempts a ON a.id=c.attempt_id WHERE a.worker_id=?", workerID).Scan(&count)
	if err != nil {
		return false, err
	}
	human, err := t.CountControllerOccupancy(workerID)
	return count > 0 || human > 0, err
}

type AdmissionLimits struct {
	MaxActiveSlots       int
	ControlReservedSlots int
}

func (t *Tx) ActiveBusinessSlotCount() (int, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT (SELECT count(*) FROM carrier_occupancy o JOIN attempts a ON a.id=o.attempt_id JOIN contexts c ON c.id=a.context_id WHERE COALESCE(json_extract(c.payload,'$.purpose'),'')<>'control')+(SELECT count(*) FROM native_maintenance WHERE state IN ('sending','unknown'))`).Scan(&count)
	return count, err
}
func (t *Tx) CheckAdmissionCapacity(purpose work.ContextPurpose, limits AdmissionLimits) error {
	if limits.MaxActiveSlots == 0 && limits.ControlReservedSlots == 0 {
		return nil
	}
	if limits.MaxActiveSlots < 1 || limits.ControlReservedSlots < 0 || limits.ControlReservedSlots >= limits.MaxActiveSlots {
		return fmt.Errorf("invalid execution limits")
	}
	count, err := t.ActiveSlotCount()
	if err != nil {
		return err
	}
	if count >= limits.MaxActiveSlots {
		return fmt.Errorf("all execution slots occupied")
	}
	if purpose != work.PurposeControl && limits.ControlReservedSlots > 0 {
		business, err := t.ActiveBusinessSlotCount()
		if err != nil {
			return err
		}
		if business >= limits.MaxActiveSlots-limits.ControlReservedSlots {
			return fmt.Errorf("business execution slots occupied; control capacity is reserved")
		}
	}
	return nil
}
