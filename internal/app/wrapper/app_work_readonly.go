package wrapper

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func validateReadOnlyCommand(command agentproto.Command) error {
	if command.Target.SourceThreadID != "" {
		return fmt.Errorf("read-only review cannot borrow another native history")
	}
	switch command.Kind {
	case agentproto.CommandPromptSend, agentproto.CommandRuntimeInspect, agentproto.CommandModelList, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt:
		return nil
	}
	return fmt.Errorf("command is outside the fixed read-only review capability")
}

func (r *workExecutionRuntime) enforceReadOnlyPhases(phases []runtimeCommandPhase) error {
	for i := range phases {
		if err := r.enforceReadOnlyFrames(phases[i].OutboundToChild); err != nil {
			return err
		}
	}
	return nil
}

func (r *workExecutionRuntime) enforceReadOnlyFrames(frames [][]byte) error {
	if !r.work.snapshot.ReadOnly || r.Backend() != agentproto.BackendCodex {
		return nil
	}
	for i, raw := range frames {
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			return err
		}
		params, _ := frame["params"].(map[string]any)
		switch frame["method"] {
		case "thread/start", "thread/resume":
			if params == nil {
				return fmt.Errorf("review thread lacks explicit native parameters")
			}
			params["sandbox"] = "read-only"
			params["approvalPolicy"] = "never"
		case "turn/start":
			if params == nil {
				return fmt.Errorf("review turn lacks explicit native parameters")
			}
			params["sandboxPolicy"] = map[string]any{"type": "readOnly", "networkAccess": false}
			params["approvalPolicy"] = "never"
		default:
			continue
		}
		encoded, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		frames[i] = append(encoded, '\n')
	}
	return nil
}

func (a *App) applyWorkClaudeReadOnly(args []string) []string {
	if a.work == nil || !a.work.snapshot.ReadOnly {
		return args
	}
	for _, key := range []string{"--allow-dangerously-skip-permissions", "--dangerously-skip-permissions", "--permission-mode", "--tools", "--allowedTools", "--disallowedTools", "--mcp-config", "--settings"} {
		args = removeArgWithOptionalValue(args, key)
	}
	settings := map[string]any{"disableAllHooks": true}
	if profile, ok := a.readClaudeRuntimeSettingsOverlay(); ok {
		settings["env"] = profile.Env
	}
	encoded, _ := json.Marshal(settings)
	return append(args, "--tools", "Read,Glob,Grep", "--disallowedTools", "Bash,PowerShell,Edit,Write,MultiEdit,NotebookEdit,Task,Agent", "--permission-mode", "plan", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--disable-slash-commands", "--settings", string(encoded))
}
