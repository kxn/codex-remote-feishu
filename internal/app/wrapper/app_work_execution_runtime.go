package wrapper

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type workExecutionRuntime struct {
	backendRuntime
	work *workExecutionState
}

func (r *workExecutionRuntime) TranslateCommand(command agentproto.Command) (runtimeCommandResult, error) {
	command, err := r.work.prepareCommand(command, r.Backend())
	if err != nil {
		return runtimeCommandResult{}, err
	}
	if command.Kind == agentproto.CommandRuntimeMCPRead {
		return r.translateMCPRead(command)
	}
	if r.Backend() == agentproto.BackendClaude && command.Kind == agentproto.CommandModelList {
		events := []agentproto.Event{r.work.claudeModels(command)}
		r.work.annotateEvents(events)
		return runtimeCommandResult{Events: events}, nil
	}
	result, err := r.backendRuntime.TranslateCommand(command)
	if err == nil {
		err = r.stampUserUUIDs(command, &result)
	}
	r.work.annotateEvents(result.Events)
	if err == nil {
		err = r.enforceReadOnlyPhases(result.Phases)
	}
	if err == nil {
		err = r.work.registerFrames(command.CommandID, flattenRuntimeCommandPhaseFrames(result.Phases))
	}
	if err == nil {
		err = r.work.inspection.persist(r.work.journal, r.work.snapshot.Owner)
	}
	return result, err
}

func (r *workExecutionRuntime) ObserveClient(line []byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{Suppress: true}, workCommandProblem(agentproto.Command{}, "work_parent_input_not_allowed", "受管 WorkerSession 只接收携带明确执行绑定的 relay 命令。")

}

func (r *workExecutionRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	if r.work.inspection.raw(line) {
		r.work.mu.Lock()
		delete(r.work.nativeRequests, nativeResponseRequestID(line))
		r.work.mu.Unlock()
		err := r.work.inspection.persist(r.work.journal, r.work.snapshot.Owner)
		return runtimeObserveResult{Suppress: true}, err
	}
	if event, handled := r.observeMCPReply(line); handled {
		events := []agentproto.Event{event}
		r.work.annotateEvents(events)
		return runtimeObserveResult{Suppress: true, Events: events}, r.work.inspection.persist(r.work.journal, r.work.snapshot.Owner)
	}
	result, err := r.backendRuntime.ObserveServer(line)
	result.Events = append(result.Events, r.work.inspection.drainReceipts()...)
	r.work.annotateEvents(result.Events)
	r.work.inspection.events(result.Events)
	result.Events = r.work.maintenanceEvents(result.Events)
	if err == nil {
		err = r.enforceReadOnlyFrames(result.OutboundToChild)
	}
	if err == nil && r.work.journal != nil {
		requestID := nativeResponseRequestID(line)
		r.work.mu.Lock()
		commandID := r.work.nativeRequests[requestID]
		delete(r.work.nativeRequests, requestID)
		r.work.mu.Unlock()
		err = r.work.registerFrames(commandID, result.OutboundToChild)
	}
	if err == nil {
		err = r.work.inspection.persist(r.work.journal, r.work.snapshot.Owner)
	}
	return result, err
}

func (r *workExecutionRuntime) Cleanup() {
	cleanupBackendRuntime(r.backendRuntime)
	if r.work.journal != nil {
		_ = r.work.journal.Close()
	}
	r.work.inspection.mu.Lock()
	scopes := append([]*hostinventory.Scope(nil), r.work.inspection.scopes...)
	r.work.inspection.mu.Unlock()
	for _, scope := range scopes {
		_ = scope.Close()
	}
}

func (a *App) workProtocolVersion() int {
	if a.work != nil && a.workErr == nil {
		return agentproto.WorkerProtocolVersion
	}
	return 0
}

func (a *App) claudeInstructionText() string {
	if a.work != nil {
		return a.work.snapshot.InstructionBundle.Text
	}
	return strings.TrimSpace(os.Getenv(config.ClaudeAppendSystemPromptEnv))
}

func (a *App) applyWorkChildEnv(base []string) []string {
	env := make([]string, 0, len(base)+4)
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case feishuMCPBearerEnvName:
			if a.work != nil || a.config.WorkLaunchSnapshot != nil {
				continue
			}
		case config.WorkLaunchSnapshotEnv, config.WorkSessionTokenEnv, config.WorkToolEndpointEnv, config.WorkSessionCredentialRefEnv, config.WorkOwnerBindingEnv:
			continue
		}
		env = append(env, item)
	}
	if a.work == nil {
		return env
	}
	if a.config.Backend == agentproto.BackendClaude {
		env = upsertEnvValue(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY", "1")
	}
	owner, _ := json.Marshal(a.work.snapshot.Owner)
	env = append(env,
		config.WorkToolEndpointEnv+"="+a.work.snapshot.ToolEndpoint,
		config.WorkSessionCredentialRefEnv+"="+a.work.snapshot.SessionCredentialRef,
		config.WorkOwnerBindingEnv+"="+string(owner),
	)
	if a.work.token != "" {
		env = append(env, config.WorkSessionTokenEnv+"="+a.work.token)
	}
	return env
}
