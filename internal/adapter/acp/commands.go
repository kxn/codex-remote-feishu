package acp

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) translatePromptSend(command agentproto.Command) (Result, error) {
	mode := command.Target.EffectivePromptExecutionMode()
	threadID := strings.TrimSpace(command.Target.ThreadID)
	if mode == agentproto.PromptExecutionModeForkEphemeral {
		return t.translatePromptFork(command)
	}
	if mode == agentproto.PromptExecutionModeStartNew || mode == agentproto.PromptExecutionModeStartEphemeral || threadID == "" {
		requestID := t.NextRequest("session-new")
		t.pendingRPC[requestID] = pendingRPC{Kind: "session/new", Command: command}
		frame, err := marshalLine(map[string]any{
			"jsonrpc": "2.0",
			"id":      requestID,
			"method":  "session/new",
			"params": map[string]any{
				"cwd":        t.commandCWD(command),
				"mcpServers": t.mcpServersParam(),
			},
		})
		if err != nil {
			return Result{}, err
		}
		return Result{OutboundToChild: [][]byte{frame}}, nil
	}
	if t.currentSessionID != threadID {
		requestID := t.NextRequest("session-resume")
		t.pendingRPC[requestID] = pendingRPC{Kind: "session/resume", Command: command}
		frame, err := marshalLine(map[string]any{
			"jsonrpc": "2.0",
			"id":      requestID,
			"method":  "session/resume",
			"params": map[string]any{
				"cwd":        t.commandCWD(command),
				"sessionId":  threadID,
				"mcpServers": t.mcpServersParam(),
			},
		})
		if err != nil {
			return Result{}, err
		}
		return Result{OutboundToChild: [][]byte{frame}}, nil
	}
	return t.startPromptForSession(threadID, command)
}

func opencodeACPModeForPlanOverride(value string) (string, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", false, nil
	case "on", "plan":
		return "plan", true, nil
	case "off", "build":
		return "build", true, nil
	default:
		return "", false, fmt.Errorf("unsupported OpenCode plan mode override %q", value)
	}
}

func (t *Translator) translatePromptFork(command agentproto.Command) (Result, error) {
	sourceThreadID := xutil.FirstNonEmpty(command.Target.SourceThreadID, command.Target.ThreadID)
	if sourceThreadID == "" {
		return Result{}, fmt.Errorf("prompt.send fork_ephemeral requires source thread id")
	}
	requestID := t.NextRequest("session-fork")
	t.pendingRPC[requestID] = pendingRPC{Kind: "session/fork", Command: command}
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "session/fork",
		"params": map[string]any{
			"cwd":        t.commandCWD(command),
			"sessionId":  sourceThreadID,
			"mcpServers": t.mcpServersParam(),
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OutboundToChild: [][]byte{frame}}, nil
}

func (t *Translator) translateTurnInterrupt(command agentproto.Command) (Result, error) {
	sessionID := xutil.FirstNonEmpty(command.Target.ThreadID, t.currentSessionID)
	if sessionID == "" {
		return Result{}, nil
	}
	if turn := t.activeTurns[sessionID]; turn != nil {
		turn.Completed = true
	}
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params": map[string]any{
			"sessionId": sessionID,
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OutboundToChild: [][]byte{frame}}, nil
}

func (t *Translator) translateRequestRespond(command agentproto.Command) (Result, error) {
	requestID := strings.TrimSpace(command.Request.RequestID)
	if requestID == "" {
		return Result{}, nil
	}
	pending, ok := t.pendingPermissions[requestID]
	if !ok {
		return Result{}, agentproto.ErrorInfo{
			Code:             "opencode_request_not_found",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "OpenCode runtime 找不到要响应的 request。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
			RequestID:        requestID,
		}
	}
	optionID := resolvePermissionOptionID(command.Request.Response)
	if optionID == "" {
		optionID = "reject"
	}
	switch permissionApprovalGrant(optionID, pending.Options) {
	case "once":
		t.writeApprovals[pending.ThreadID] = writeApproval{Remaining: 1}
	case "always":
		t.writeApprovals[pending.ThreadID] = writeApproval{Always: true}
	}
	delete(t.pendingPermissions, requestID)
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      pending.NativeID,
		"result": map[string]any{
			"outcome": map[string]any{
				"outcome":  "selected",
				"optionId": optionID,
			},
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Events: []agentproto.Event{{
			Kind:      agentproto.EventRequestResolved,
			ThreadID:  pending.ThreadID,
			TurnID:    pending.TurnID,
			RequestID: requestID,
			Status:    "resolved",
		}},
		OutboundToChild: [][]byte{frame},
	}, nil
}

func (t *Translator) translateThreadsRefresh(command agentproto.Command) (Result, error) {
	requestID := t.NextRequest("session-list")
	t.pendingRPC[requestID] = pendingRPC{Kind: "session/list", Command: command}
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "session/list",
		"params": map[string]any{
			"cwd": t.commandCWD(command),
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OutboundToChild: [][]byte{frame}}, nil
}

func (t *Translator) translateThreadHistoryRead(command agentproto.Command) (Result, error) {
	sessionID := strings.TrimSpace(command.Target.ThreadID)
	if sessionID == "" {
		return Result{}, fmt.Errorf("thread.history.read requires thread id")
	}
	t.historyHydrations[sessionID] = newHistoryHydration(command, t.commandCWD(command))
	requestID := t.NextRequest("session-load")
	t.pendingRPC[requestID] = pendingRPC{Kind: "session/load", Command: command}
	frame, err := marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "session/load",
		"params": map[string]any{
			"cwd":        t.commandCWD(command),
			"sessionId":  sessionID,
			"mcpServers": t.mcpServersParam(),
		},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{OutboundToChild: [][]byte{frame}}, nil
}

func (t *Translator) translateModelList(command agentproto.Command) (Result, error) {
	session := t.sessions[t.currentSessionID]
	snapshot := agentproto.ModelCatalogSnapshot{
		IncludeHidden: command.ModelList.IncludeHidden,
		RefreshedAt:   time.Now().UTC(),
	}
	for _, option := range session.ModelOptions {
		if strings.TrimSpace(option.Value) == "" {
			continue
		}
		snapshot.Entries = append(snapshot.Entries, agentproto.ModelCatalogEntry{
			ID:          option.Value,
			Model:       option.Value,
			DisplayName: xutil.FirstNonEmpty(option.Name, option.Value),
			IsDefault:   option.Value == session.CurrentModel,
		})
	}
	if len(snapshot.Entries) == 0 {
		snapshot.Unsupported = true
		snapshot.ErrorMessage = "OpenCode 尚未返回可用模型配置。"
	}
	return Result{Events: []agentproto.Event{{
		Kind:         agentproto.EventModelCatalogUpdated,
		CommandID:    command.CommandID,
		ModelCatalog: &snapshot,
	}}}, nil
}
