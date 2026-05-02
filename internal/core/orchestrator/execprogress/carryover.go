package execprogress

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func RolloverCarryoverEntries(progress *state.ExecCommandProgressRecord, startSeq int) {
	if progress == nil || startSeq <= 0 {
		return
	}
	maxSeq := progress.LastVisibleSeq
	maxSeq = rolloverCarryoverBlock(progress.ProcessPlan, startSeq, maxSeq)
	maxSeq = rolloverCarryoverExploration(progress.Exploration, startSeq, maxSeq)
	type carryoverCandidate struct {
		Index   int
		LastSeq int
	}
	candidates := make([]carryoverCandidate, 0, len(progress.Entries))
	for index := range progress.Entries {
		entry := progress.Entries[index]
		if entry.LastSeq > maxSeq {
			maxSeq = entry.LastSeq
		}
		if entry.LastSeq >= startSeq || !carryoverEligibleEntry(entry) {
			continue
		}
		candidates = append(candidates, carryoverCandidate{
			Index:   index,
			LastSeq: entry.LastSeq,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].LastSeq != candidates[j].LastSeq {
			return candidates[i].LastSeq < candidates[j].LastSeq
		}
		return candidates[i].Index < candidates[j].Index
	})
	for _, candidate := range candidates {
		maxSeq++
		progress.Entries[candidate.Index].LastSeq = maxSeq
	}
	if maxSeq > progress.LastVisibleSeq {
		progress.LastVisibleSeq = maxSeq
	}
}

func rolloverCarryoverBlock(record *state.ExecCommandProgressPlanRecord, startSeq, maxSeq int) int {
	if record == nil || strings.ToLower(strings.TrimSpace(record.Block.Status)) != "running" {
		return maxSeq
	}
	return rolloverCarryoverBlockRows(&record.Block, startSeq, maxSeq)
}

func rolloverCarryoverExploration(record *state.ExecCommandProgressExplorationRecord, startSeq, maxSeq int) int {
	if record == nil || strings.ToLower(strings.TrimSpace(record.Block.Status)) != "running" {
		return maxSeq
	}
	return rolloverCarryoverBlockRows(&record.Block, startSeq, maxSeq)
}

func rolloverCarryoverBlockRows(block *state.ExecCommandProgressBlockRecord, startSeq, maxSeq int) int {
	if block == nil || startSeq <= 0 {
		return maxSeq
	}
	candidates := make([]int, 0, len(block.Rows))
	for index := range block.Rows {
		row := block.Rows[index]
		if row.LastSeq > maxSeq {
			maxSeq = row.LastSeq
		}
		if row.LastSeq >= startSeq || !carryoverEligibleBlockRow(row) {
			continue
		}
		candidates = append(candidates, index)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := block.Rows[candidates[i]]
		right := block.Rows[candidates[j]]
		if left.LastSeq != right.LastSeq {
			return left.LastSeq < right.LastSeq
		}
		return candidates[i] < candidates[j]
	})
	for _, index := range candidates {
		maxSeq++
		block.Rows[index].LastSeq = maxSeq
	}
	return maxSeq
}

func carryoverEligibleEntry(entry state.ExecCommandProgressEntryRecord) bool {
	switch strings.ToLower(strings.TrimSpace(entry.Status)) {
	case "running", "started":
	default:
		return false
	}
	switch strings.TrimSpace(entry.Kind) {
	case "command_execution", "context_compaction", "delegated_task", "dynamic_tool_call", "file_change", "mcp_tool_call", "reasoning_summary", "web_search":
		return true
	default:
		return false
	}
}

func carryoverEligibleBlockRow(row state.ExecCommandProgressBlockRowRecord) bool {
	switch strings.TrimSpace(row.Kind) {
	case "read", "list", "search":
		return true
	default:
		return strings.TrimSpace(row.Summary) != "" || len(row.Items) != 0
	}
}
