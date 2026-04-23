package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func applyThreadRuntimeStatus(thread *state.ThreadRecord, runtime *agentproto.ThreadRuntimeStatus) {
	if thread == nil || runtime == nil {
		return
	}
	thread.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(runtime)
	thread.Loaded = runtime.IsLoaded()
}

func markThreadNotLoaded(thread *state.ThreadRecord) {
	if thread == nil {
		return
	}
	applyThreadRuntimeStatus(thread, &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded})
}

func threadRuntimeStatusType(thread *state.ThreadRecord) string {
	if thread == nil || thread.RuntimeStatus == nil {
		return ""
	}
	return string(thread.RuntimeStatus.Type)
}

func threadWaitingOnApproval(thread *state.ThreadRecord) bool {
	return thread != nil && thread.RuntimeStatus != nil && thread.RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnApproval)
}

func threadWaitingOnUserInput(thread *state.ThreadRecord) bool {
	return thread != nil && thread.RuntimeStatus != nil && thread.RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnUserInput)
}

func threadRuntimeActive(thread *state.ThreadRecord) bool {
	return thread != nil && thread.RuntimeStatus != nil && thread.RuntimeStatus.Type == agentproto.ThreadRuntimeStatusTypeActive
}

func threadLegacyState(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	if thread.RuntimeStatus != nil {
		if legacy := strings.TrimSpace(thread.RuntimeStatus.LegacyState()); legacy != "" {
			return legacy
		}
	}
	return strings.TrimSpace(thread.State)
}
