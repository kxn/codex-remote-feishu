package execprogress

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// DelegatedTaskEntry projects the same public task identity on every surface.
func DelegatedTaskEntry(event agentproto.Event) state.ExecCommandProgressEntryRecord {
	entry := state.ExecCommandProgressEntryRecord{
		ItemID: strings.TrimSpace(event.ItemID), Kind: "delegated_task", Label: "Task",
		Summary: strings.TrimSpace(xutil.MetadataString(event.Metadata, "description")),
		Status:  NormalizeStatus(event.Status, event.Kind == agentproto.EventItemCompleted),
	}
	subagentType := strings.TrimSpace(xutil.MetadataString(event.Metadata, "subagentType"))
	switch {
	case entry.Summary != "" && subagentType != "":
		entry.Summary = subagentType + " · " + entry.Summary
	case entry.Summary == "" && subagentType != "":
		entry.Summary = subagentType
	case entry.Summary == "":
		entry.Summary = "任务处理中"
	}
	return entry
}
