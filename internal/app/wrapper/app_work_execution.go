package wrapper

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
)

const maxWorkLaunchSnapshotBytes = 1 << 20

type workExecutionState struct {
	inspection     *workInspectionState
	journal        *runtimejournal.Store
	frames         map[*byte]workFrameReference
	nativeRequests map[string]string
	snapshot       agentproto.RuntimeLaunchSnapshot
	token          string
	mu             sync.Mutex
	commands       map[string]agentproto.ExecutionBinding
	maintenance    *workMaintenance
	mcpRequests    map[string]agentproto.Command
}

func readWorkLaunchSnapshot(path string) (*agentproto.RuntimeLaunchSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("work launch snapshot must be a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open work launch snapshot")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() > maxWorkLaunchSnapshotBytes {
		return nil, fmt.Errorf("work launch snapshot file changed or exceeds size limit")
	}
	if runtime.GOOS != "windows" && opened.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("work launch snapshot requires owner-only file permissions")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxWorkLaunchSnapshotBytes+1))
	decoder.DisallowUnknownFields()
	var snapshot agentproto.RuntimeLaunchSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("invalid work launch snapshot JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("work launch snapshot must contain one JSON object")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func newWorkExecutionState(cfg Config) (*workExecutionState, error) {
	if cfg.WorkLaunchSnapshot == nil {
		return nil, nil
	}
	snapshot := *cfg.WorkLaunchSnapshot
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Managed || cfg.Source != "headless" || (cfg.Backend != agentproto.BackendCodex && cfg.Backend != agentproto.BackendClaude && cfg.Backend != agentproto.BackendOpenCode) {
		return nil, fmt.Errorf("work execution requires a managed headless native wrapper")
	}
	if !filepath.IsAbs(snapshot.CWD) || !pathcompare.SameCleanPlatformPath(snapshot.CWD, cfg.WorkspaceRoot) {
		return nil, fmt.Errorf("work launch cwd requires a new matching wrapper")
	}
	if snapshot.SessionCredentialRef != "" && snapshot.SessionCredentialRef != "env:"+config.WorkSessionTokenEnv {
		return nil, fmt.Errorf("unsupported work session credential reference")
	}
	token := os.Getenv(config.WorkSessionTokenEnv)
	if snapshot.ToolEndpoint != "" && (snapshot.SessionCredentialRef == "" || token == "") {
		return nil, fmt.Errorf("work tool endpoint requires a separately supplied session credential")
	}
	work := &workExecutionState{snapshot: snapshot, token: token, commands: make(map[string]agentproto.ExecutionBinding), inspection: newWorkInspectionState()}
	if err := work.initializeJournal(cfg); err != nil {
		return nil, err
	}
	return work, nil
}

func workCommandProblem(command agentproto.Command, code, message string) error {
	return agentproto.ErrorInfo{
		Code: code, Layer: "wrapper", Stage: "validate_work_execution", Operation: string(command.Kind),
		Message: message, CommandID: command.CommandID, ThreadID: command.Target.ThreadID,
	}
}

func (a *App) prepareWorkCommand(command agentproto.Command) (agentproto.Command, error) {
	if a.work == nil {
		if command.Execution != nil {
			return command, workCommandProblem(command, "work_execution_not_supported", "当前 wrapper 未绑定 WorkerSession，无法接收团队执行命令。")
		}
		return command, nil
	}
	return a.work.prepareCommand(command, a.config.Backend)
}

func (w *workExecutionState) prepareCommand(command agentproto.Command, backend agentproto.Backend) (agentproto.Command, error) {
	if w.snapshot.ReadOnly {
		if err := validateReadOnlyCommand(command); err != nil {
			return command, err
		}
	}
	ownerOnly := command.IsOwnerOnlyRuntimeCommand()
	if command.Execution == nil || (ownerOnly && command.Execution.ValidateOwner() != nil) || (!ownerOnly && command.Execution.ValidateExecution() != nil) || strings.TrimSpace(command.CommandID) == "" {
		return command, workCommandProblem(command, "work_execution_binding_required", "团队命令必须携带完整执行绑定、Attempt 和 run_ref。")
	}
	if !w.snapshot.Owner.SameOwner(*command.Execution) {
		return command, workCommandProblem(command, "work_execution_owner_mismatch", "团队执行身份或代次已变化，当前 wrapper 拒绝此命令。")
	}
	if command.Target.CWD != "" && !pathcompare.SameCleanPlatformPath(command.Target.CWD, w.snapshot.CWD) {
		return command, workCommandProblem(command, "work_execution_rebuild_required", "执行目录已变化，需要新的匹配 wrapper。")
	}
	if command.Kind == agentproto.CommandPromptSend && strings.TrimSpace(command.Target.CWD) == "" {
		return command, workCommandProblem(command, "work_execution_cwd_required", "团队工作输入必须携带确定的执行目录。")
	}
	if (command.Kind == agentproto.CommandPromptSend || command.InstructionRevision != "") && command.InstructionRevision != w.snapshot.InstructionBundle.Revision {
		return command, workCommandProblem(command, "work_instruction_rebuild_required", "常驻说明版本已变化，需要新的匹配 wrapper。")
	}
	if backend == agentproto.BackendCodex && (command.Kind == agentproto.CommandPromptSend || command.CodexResume != nil) {
		policy := agentproto.CloneCodexResumePolicy(command.CodexResume)
		if policy == nil {
			return command, workCommandProblem(command, "work_codex_policy_required", "团队 Codex 输入必须携带确定的 profile policy。")
		}
		policy.DeveloperInstruction = w.snapshot.InstructionBundle.Text
		command.CodexResume = policy
	}
	if command.Kind == agentproto.CommandTurnSteer {
		if err := w.validateNativeSteer(command, backend); err != nil {
			return command, err
		}
	}
	if err := w.prepareMaintenance(command, backend); err != nil {
		return command, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if previous, ok := w.commands[command.CommandID]; ok && previous != *command.Execution {
		return command, workCommandProblem(command, "work_command_binding_conflict", "同一 command ID 不能绑定到另一执行。")
	}
	w.commands[command.CommandID] = *command.Execution
	command.Execution = agentproto.CloneExecutionBinding(command.Execution)
	return command, nil
}

func (w *workExecutionState) annotateEvents(events []agentproto.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range events {
		// A thread, turn, or the newest attempt alone is insufficient proof of ownership.
		events[i].Execution = nil
		if events[i].CommandID == "" {
			continue
		}
		if binding, ok := w.commands[events[i].CommandID]; ok {
			events[i].Execution = agentproto.CloneExecutionBinding(&binding)
		} else if w.journal != nil {
			if binding, err := w.journal.Binding(events[i].CommandID); err == nil {
				events[i].Execution = binding
			}
		}
	}
}
