package workstore

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) CountControllerOccupancy(workerID string) (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM runtime_controller_occupancy WHERE worker_id=?`, workerID).Scan(&n)
	return n, err
}
func (t *Tx) AssertRuntimeResourceAvailable(key string) error {
	if err := t.AssertMigrationResourceAvailable(key); err != nil {
		return err
	}
	var worker string
	err := t.tx.QueryRowContext(t.ctx, `SELECT worker_id FROM runtime_controller_occupancy WHERE resource_key=?`, key).Scan(&worker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("resource reserved by human controller or handoff for worker %s", worker)
}

func (t *Tx) ReserveControllerLocation(worker work.WorkerSession, nextControllerGeneration int64) error {
	return t.atomic(func() error { return t.reserveControllerLocation(worker, nextControllerGeneration) })
}

func (t *Tx) reserveControllerLocation(worker work.WorkerSession, nextControllerGeneration int64) error {
	if err := t.AssertMaintenanceResourceAvailable("worker:" + worker.ID); err != nil {
		return err
	}
	if nextControllerGeneration != worker.ControllerGeneration+1 {
		return fmt.Errorf("controller reservation generation mismatch")
	}
	if worker.LocationID == "" {
		return nil
	}
	location, err := t.GetLocation(worker.LocationID)
	if err != nil {
		return err
	}
	if location.Generation != worker.LocationGeneration {
		return fmt.Errorf("controller reservation location changed")
	}
	keys := append([]string{"cwd:" + location.HostID + ":" + location.Cwd}, location.ResourceKeys...)
	for _, key := range keys {
		if err := t.AssertMaintenanceResourceAvailable(key); err != nil {
			return err
		}
		var otherWorker string
		err := t.tx.QueryRowContext(t.ctx, `SELECT json_extract(a.payload,'$.run_ref.worker_id') FROM resource_occupancy o JOIN attempts a ON a.id=o.attempt_id WHERE o.resource_key=?`, key).Scan(&otherWorker)
		if err == nil && otherWorker != worker.ID {
			return fmt.Errorf("controller location is held by another worker")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var owner string
		var generation int64
		err = t.tx.QueryRowContext(t.ctx, `SELECT worker_id,controller_generation FROM runtime_controller_occupancy WHERE resource_key=?`, key).Scan(&owner, &generation)
		if err == nil {
			if owner != worker.ID || generation != nextControllerGeneration {
				return ErrConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO runtime_controller_occupancy(resource_key,worker_id,location_generation,controller_generation) VALUES(?,?,?,?)`, key, worker.ID, worker.LocationGeneration, nextControllerGeneration); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tx) ClearControllerLocation(workerID string, controllerGeneration, locationGeneration int64) error {
	var mismatched int
	if err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM runtime_controller_occupancy WHERE worker_id=? AND (controller_generation<>? OR location_generation<>?)`, workerID, controllerGeneration, locationGeneration).Scan(&mismatched); err != nil {
		return err
	}
	if mismatched > 0 {
		return ErrConflict
	}
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM runtime_controller_occupancy WHERE worker_id=? AND controller_generation=? AND location_generation=?`, workerID, controllerGeneration, locationGeneration)
	return err
}
