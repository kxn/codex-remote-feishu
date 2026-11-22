package workdisplay

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (p *projection) progressStatus(label, text, status string) error {
	execprogress.UpsertEntry(&p.display.Progress, state.ExecCommandProgressEntryRecord{
		ItemID: "turn-status", Kind: "turn_status", Label: label, Summary: text, Status: status,
	})
	return p.publishProgress()
}

func (p *projection) item() error {
	event := p.event
	if event.ItemID == "" {
		return nil
	}
	complete := event.Kind == agentproto.EventItemCompleted
	if event.ItemKind == "image_generation" && complete {
		return p.image()
	}
	verbosity := p.display.Progress.Verbosity
	detailed := verbosity == state.SurfaceVerbosityVerbose || verbosity == state.SurfaceVerbosityChatty
	entry := state.ExecCommandProgressEntryRecord{ItemID: "native:" + event.ItemID, Kind: event.ItemKind, Status: execprogress.NormalizeStatus(event.Status, complete)}
	switch event.ItemKind {
	case "file_change":
		if execprogress.UpsertFileChangeProgressEntries(&p.display.Progress, event, complete) {
			return p.publishProgress()
		}
		return nil
	case "context_compaction":
		entry.Summary = "正在整理上下文"
		if complete {
			entry.Summary = "已整理上下文"
			if entry.Status == "failed" {
				entry.Summary = "上下文整理失败"
			}
		}
	case "reasoning_summary", "reasoning":
		if !detailed {
			return nil
		}
		entry.ItemID, entry.Kind, entry.Label, entry.Summary = "reasoning-latest", "reasoning_placeholder", "思考", "正在分析"
		if complete {
			entry.Summary = "已完成分析"
		}
		if event.Kind == agentproto.EventItemReasoningSummaryPartAdded {
			execprogress.UpsertReasoning(&p.display.Progress, event, agentproto.BackendCodex, p.now)
			return p.publishProgress()
		}
		streamed, _ := event.Metadata["publicSummaryStreamed"].(bool)
		if streamed && p.display.Progress.Reasoning != nil {
			p.display.Progress.Reasoning.Active = false
			execprogress.FreezeReasoningForVisibleAction(&p.display.Progress)
			return p.publishProgress()
		}
		if public := xutil.MetadataString(event.Metadata, "publicSummary"); public != "" {
			event.Delta = public
			event.Metadata = nil
			execprogress.UpsertReasoning(&p.display.Progress, event, agentproto.BackendCodex, p.now)
			return p.publishProgress()
		}
	case "web_search":
		if !detailed {
			return nil
		}
		entry = execprogress.WebSearchEntry(event.Metadata, complete)
		entry.ItemID = "native:" + event.ItemID
	case "command_execution":
		if !detailed {
			return nil
		}
		if event.Exploration != nil {
			resolution := execprogress.ResolveExplorationProgressForCommandExecution(&p.display.Progress, event, complete)
			if resolution.Disposition == execprogress.ExplorationDispositionStructured {
				return p.publishProgress()
			}
			if resolution.Disposition == execprogress.ExplorationDispositionPending {
				return nil
			}
		}
		command, _ := execprogress.CommandMetadata(event)
		entry.Kind, entry.Label = "tool_activity", "执行"
		if preview := publicCommandPreview(command); preview != "" {
			entry.Summary = activityStatus(entry.Status, preview) + "（参数未显示）"
		} else {
			entry.Summary = activityStatus(entry.Status, "自定义命令")
		}

	case "mcp_tool_call":
		entry.Label = "MCP"
		server := xutil.MetadataString(event.Metadata, "server")
		tool := xutil.MetadataString(event.Metadata, "tool")
		switch {
		case server != "" && tool != "":
			entry.Summary = server + "/" + tool
		default:
			entry.Summary = xutil.FirstNonEmpty(tool, server)
		}
		entry.Summary = activityStatus(entry.Status, xutil.FirstNonEmpty(entry.Summary, "调用"))
	case "dynamic_tool_call":
		if !detailed {
			return nil
		}
		// The public tool name and typed exploration are available without
		// forwarding tool arguments, results, or arbitrary JSON metadata.
		event.Metadata = map[string]any{"tool": xutil.MetadataString(event.Metadata, "tool")}
		resolution := execprogress.ResolveExplorationProgressForDynamicTool(&p.display.Progress, event, complete)
		if resolution.Disposition == execprogress.ExplorationDispositionStructured {
			return p.publishProgress()
		}
		if resolution.Disposition == execprogress.ExplorationDispositionPending {
			return nil
		}
		entry, _, _ = execprogress.UpsertDynamicToolProgressEntry(&p.display.Progress, event)
	case "delegated_task":
		entry.Label = "Task"
		entry.Summary = xutil.FirstNonEmpty(xutil.MetadataString(event.Metadata, "description"), xutil.MetadataString(event.Metadata, "subagentType"), "子任务")
	case "image_generation":
		return nil // The completed event sends the actual image.
	case "plan":
		if text := xutil.MetadataString(event.Metadata, "text"); complete && text != "" {
			return p.reply("执行计划", text, nil, "")
		}
		return nil
	default:
		return nil
	}
	execprogress.UpsertEntry(&p.display.Progress, entry)
	return p.publishProgress()
}

// A generic command can carry arbitrary secrets. Project only known tool
// identities and vocabulary, even when no recognizable credential flag exists.
func publicCommandPreview(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	name := commandExecutable(fields[0])
	if slices.Contains([]string{"bash", "sh", "zsh", "fish", "pwsh"}, name) &&
		len(fields) >= 3 && slices.Contains([]string{"-c", "-lc", "-Command"}, fields[1]) {
		fields = fields[2:]
		name = commandExecutable(fields[0])
	}
	switch name {
	case "go", "git", "gh", "docker", "cargo", "npm", "pnpm", "yarn":
		if len(fields) > 1 {
			action := strings.Trim(fields[1], "\"'")
			if slices.Contains([]string{"test", "build", "run", "status", "diff", "log", "show", "fetch", "list", "check", "fmt", "install", "view"}, action) {
				return name + " " + action
			}
		}
		return name
	case "rg", "grep", "ls", "cat", "sed", "find", "head", "tail", "curl", "wget", "python", "python3", "node", "make", "cmake", "pytest", "jq", "tar", "unzip", "bash", "sh", "pwsh":
		return name
	default:
		return ""
	}
}

func commandExecutable(token string) string {
	return strings.ToLower(filepath.Base(strings.Trim(token, "\"'")))
}

func activityStatus(status, activity string) string {
	switch status {
	case "completed":
		return activity + "已完成"
	case "failed":
		return activity + "失败"
	case "interrupted":
		return activity + "已停止"
	default:
		return activity + "进行中"
	}
}

func (p *projection) plan() error {
	snapshot := p.event.PlanSnapshot
	if snapshot == nil {
		return nil
	}
	plan := &control.PlanUpdate{ThreadID: p.event.ThreadID, TurnID: p.event.TurnID, Explanation: strings.TrimSpace(snapshot.Explanation)}
	for _, step := range snapshot.Steps {
		if strings.TrimSpace(step.Step) != "" {
			plan.Steps = append(plan.Steps, control.PlanUpdateStep{Step: step.Step, Status: step.Status})
		}
	}
	p.display.Plan = plan
	return p.publishProgress()
}

func (p *projection) collectFiles() {
	if execFailed(p.event.Status) {
		return
	}
	for _, change := range p.event.FileChanges {
		if strings.TrimSpace(change.Path) == "" {
			continue
		}
		added, removed := execprogress.FileChangeLineCounts(change)
		if p.display.Files == nil {
			p.display.Files = make(map[string]control.FileChangeSummaryEntry)
		}
		p.display.Files[change.Path] = control.FileChangeSummaryEntry{Path: change.Path, MovePath: change.MovePath, AddedLines: added, RemovedLines: removed}
	}
}

func (p *projection) turnDiff() error {
	if strings.TrimSpace(p.event.TurnDiff) == "" {
		return nil
	}
	added, removed := execprogress.UnifiedDiffLineCounts(p.event.TurnDiff)
	execprogress.UpsertEntry(&p.display.Progress, state.ExecCommandProgressEntryRecord{
		ItemID: "turn-diff", Kind: "changes", Label: "本轮文件变更",
		Summary: fmt.Sprintf("新增 %d 行，删除 %d 行", added, removed), Status: "completed",
	})
	return p.publishProgress()
}

func (p *projection) publishProgress() error {
	v := p.display
	progress := execprogress.Snapshot(&v.Progress)
	if progress == nil {
		return nil
	}
	for i := range progress.Timeline {
		if progress.Timeline[i].Kind == "reasoning_summary" {
			progress.Timeline[i].Summary = feishu.PrivatePublicSummary(progress.Timeline[i].Summary)
		}
	}
	title := "工作中"
	if v.Terminated {
		title = map[string]string{"completed": "执行完成", "failed": "执行失败", "interrupted": "执行已停止"}[p.event.Status]
	}
	card, startSeq, rollover := feishu.PrivateExecutionProgress(*progress, v.Plan, title)
	if len(card) == 0 {
		return nil
	}
	hash := workCardHash(card)
	if hash == v.ProgressHash && !rollover {
		return nil
	}
	anchor := v.ProgressAnchorID
	fence, err := p.compactPendingProgress()
	if err != nil {
		return err
	}
	if rollover {
		anchor, fence = "", ""
	}
	id, err := p.enqueueOutput(card, nil, workservice.PrivateDisplayPublication{DisplayID: v.ID, PreviousOutboxID: v.LastOutboxID, AnchorOutboxID: anchor, PatchFenceID: fence, Progress: true})
	if err != nil {
		return err
	}
	if anchor == "" {
		v.ProgressAnchorID = id
		v.Progress.Segments = []state.ExecCommandProgressSegmentRecord{{SegmentID: id, MessageID: id, StartSeq: startSeq}}
	}
	v.ProgressFenceID, v.ProgressHash = id, hash
	return nil
}

func workCardHash(card []byte) string {
	return stableID("card", string(card))
}
