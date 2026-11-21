package workstore

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) GetNativeLaunchSnapshot(workerID string, generation int64) (agentproto.RuntimeLaunchSnapshot, error) {
	var snapshot agentproto.RuntimeLaunchSnapshot
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM operations WHERE kind='worker.launch' AND json_extract(payload,'$.target_id')=? AND json_extract(payload,'$.target_generation')=? LIMIT 1`, workerID, generation).Scan(&raw)
	if err != nil {
		return snapshot, err
	}
	var operation work.Operation
	if err = json.Unmarshal([]byte(raw), &operation); err != nil {
		return snapshot, err
	}
	body, err := t.GetContent(operation.ScopeID, operation.RequestRef)
	if err != nil {
		return snapshot, err
	}
	var launch struct {
		Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
	}
	if err = json.Unmarshal(body.Data, &launch); err != nil {
		return snapshot, err
	}
	snapshot = launch.Snapshot
	if snapshot.Owner.WorkerSessionID != workerID || snapshot.Owner.SessionGeneration != generation {
		return snapshot, fmt.Errorf("launch snapshot worker generation mismatch")
	}
	return snapshot, snapshot.Validate()
}
