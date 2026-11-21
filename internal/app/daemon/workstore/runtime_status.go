package workstore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const runtimeStatusDetailLimit = 50

type RuntimeStatus struct {
	ProjectTotal     int                    `json:"projectTotal"`
	ContextTotal     int                    `json:"contextTotal"`
	WorkerTotal      int                    `json:"workerTotal"`
	PendingInput     int                    `json:"pendingInput"`
	RunningAttempts  int                    `json:"runningAttempts"`
	OccupiedAttempts int                    `json:"occupiedAttempts"`
	DetailLimit      int                    `json:"detailLimit"`
	Projects         []RuntimeProjectStatus `json:"projects"`
	Workers          []RuntimeWorkerStatus  `json:"workers"`
}

type RuntimeProjectStatus struct {
	ID    string            `json:"id"`
	State work.ProjectState `json:"state"`
}

type RuntimeWorkerStatus struct {
	ID      string           `json:"id"`
	State   work.WorkerState `json:"state"`
	Backend work.Backend     `json:"backend"`
}

// ReadRuntimeStatus reads counts and bounded metadata from the same snapshot.
func (s *Store) ReadRuntimeStatus(ctx context.Context) (RuntimeStatus, error) {
	status := RuntimeStatus{
		DetailLimit: runtimeStatusDetailLimit,
	}
	err := s.Within(ctx, func(t *Tx) error {
		const totals = `SELECT (SELECT COUNT(*) FROM projects),
			(SELECT COUNT(*) FROM contexts), (SELECT COUNT(*) FROM workers),
			(SELECT COUNT(*) FROM inbox WHERE state = ?),
			(SELECT COUNT(*) FROM attempts WHERE state = ?),
			(SELECT COUNT(*) FROM carrier_occupancy)`
		err := t.tx.QueryRowContext(t.ctx, totals, work.InboxPending, work.AttemptRunning).Scan(
			&status.ProjectTotal, &status.ContextTotal, &status.WorkerTotal,
			&status.PendingInput, &status.RunningAttempts, &status.OccupiedAttempts,
		)
		if err != nil {
			return fmt.Errorf("read team runtime totals: %w", err)
		}
		status.Projects, err = runtimeStatusRows(t, `SELECT id, json_extract(payload, '$.state') FROM projects ORDER BY id LIMIT ?`, func(row *sql.Rows) (RuntimeProjectStatus, error) {
			var project RuntimeProjectStatus
			err := row.Scan(&project.ID, &project.State)
			return project, err
		})
		if err != nil {
			return fmt.Errorf("read team runtime projects: %w", err)
		}
		status.Workers, err = runtimeStatusRows(t, `SELECT id, state, backend FROM workers ORDER BY id LIMIT ?`, func(row *sql.Rows) (RuntimeWorkerStatus, error) {
			var worker RuntimeWorkerStatus
			err := row.Scan(&worker.ID, &worker.State, &worker.Backend)
			return worker, err
		})
		if err != nil {
			return fmt.Errorf("read team runtime workers: %w", err)
		}
		return nil
	})
	if err != nil {
		return RuntimeStatus{}, err
	}
	return status, nil
}

func runtimeStatusRows[T any](t *Tx, query string, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := t.tx.QueryContext(t.ctx, query, runtimeStatusDetailLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}
