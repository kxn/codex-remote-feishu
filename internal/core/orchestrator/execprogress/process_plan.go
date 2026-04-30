package execprogress

import (
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const ProcessPlanBlockID = "process_plan"

func UpsertProcessPlan(progress *state.ExecCommandProgressRecord, event agentproto.Event) bool {
	if progress == nil {
		return false
	}
	snapshot := decodePlanSnapshotFromMetadata(event.Metadata)
	if snapshot == nil {
		return false
	}
	record := ensureProcessPlanRecord(progress)
	before := cloneExplorationBlock(record.Block)
	record.Snapshot = agentproto.CloneTurnPlanSnapshot(snapshot)
	record.Block.Status = NormalizeStatus(event.Status, event.Kind == agentproto.EventItemCompleted)
	if strings.TrimSpace(record.Block.Status) == "" {
		record.Block.Status = "running"
	}
	record.Block.Rows = buildProcessPlanRows(progress, snapshot)
	return !sameExecCommandProgressBlock(before, record.Block)
}

func decodePlanSnapshotFromMetadata(metadata map[string]any) *agentproto.TurnPlanSnapshot {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["planSnapshot"].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{
		Explanation: strings.TrimSpace(lookupStringFromAny(raw["explanation"])),
	}
	for _, record := range mapsFromAny(raw["steps"]) {
		step := strings.TrimSpace(lookupStringFromAny(record["step"]))
		if step == "" {
			continue
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{
			Step:   step,
			Status: agentproto.NormalizeTurnPlanStepStatus(lookupStringFromAny(record["status"])),
		})
	}
	if snapshot.Explanation == "" && len(snapshot.Steps) == 0 {
		return nil
	}
	return snapshot
}

func ensureProcessPlanRecord(progress *state.ExecCommandProgressRecord) *state.ExecCommandProgressPlanRecord {
	if progress.ProcessPlan == nil {
		progress.ProcessPlan = &state.ExecCommandProgressPlanRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: ProcessPlanBlockID,
				Kind:    "process_plan",
				Status:  "running",
			},
		}
	}
	if progress.ProcessPlan.Block.BlockID == "" {
		progress.ProcessPlan.Block.BlockID = ProcessPlanBlockID
	}
	if progress.ProcessPlan.Block.Kind == "" {
		progress.ProcessPlan.Block.Kind = "process_plan"
	}
	return progress.ProcessPlan
}

func buildProcessPlanRows(progress *state.ExecCommandProgressRecord, snapshot *agentproto.TurnPlanSnapshot) []state.ExecCommandProgressBlockRowRecord {
	rows := make([]state.ExecCommandProgressBlockRowRecord, 0, len(snapshot.Steps)+1)
	if explanation := strings.TrimSpace(snapshot.Explanation); explanation != "" {
		progress.LastVisibleSeq++
		rows = append(rows, state.ExecCommandProgressBlockRowRecord{
			RowID:    "process_plan::summary",
			Kind:     "process_plan_summary",
			Summary:  explanation,
			LastSeq:  progress.LastVisibleSeq,
			MergeKey: "process_plan::summary",
		})
	}
	for index, step := range snapshot.Steps {
		progress.LastVisibleSeq++
		rows = append(rows, state.ExecCommandProgressBlockRowRecord{
			RowID:     "process_plan::step::" + strings.TrimSpace(strconv.Itoa(index+1)),
			Kind:      "process_plan_step",
			Summary:   strings.TrimSpace(step.Step),
			Secondary: string(step.Status),
			LastSeq:   progress.LastVisibleSeq,
			MergeKey:  "process_plan::step",
		})
	}
	return rows
}
