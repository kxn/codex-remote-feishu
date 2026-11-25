package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateMCPOAuthUnverifiedAllowsRetry(t *testing.T) {
	now := time.Now().UTC()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Threads: map[string]*state.ThreadRecord{}})
	s.MaterializeSurface("surface", "gateway", "chat", "user")
	s.root.Surfaces["surface"].AttachedInstanceID = "instance"
	action := control.Action{Kind: control.ActionMCPOAuthCommand, SurfaceSessionID: "surface", ActorUserID: "user", Text: "/mcpoauth github"}
	started := s.ApplySurfaceAction(action)
	if len(started) != 1 || started[0].Command == nil {
		t.Fatalf("start: %#v", started)
	}
	command := started[0].Command.CommandID
	errEvent := agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{Code: "mcp_oauth_attempt_unverified", CommandID: command, Message: "结果未确认；使用 /mcp 刷新或 /mcpoauth 重试。"})
	out := s.ApplyAgentEvent("instance", errEvent)
	if len(out) != 1 || out[0].Notice == nil || out[0].Notice.Code != "mcp_oauth_unverified" || !strings.Contains(out[0].Notice.Text, "重试") {
		t.Fatalf("unverified: %#v", out)
	}
	retry := s.ApplySurfaceAction(action)
	if len(retry) != 1 || retry[0].Command == nil || retry[0].Command.CommandID == command {
		t.Fatalf("retry: %#v", retry)
	}
}

func TestPrivateMCPOAuthNewURLSupersedesOldAttempt(t *testing.T) {
	now := time.Now().UTC()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Threads: map[string]*state.ThreadRecord{}})
	s.MaterializeSurface("surface", "gateway", "chat", "user")
	s.root.Surfaces["surface"].AttachedInstanceID = "instance"
	action := control.Action{Kind: control.ActionMCPOAuthCommand, SurfaceSessionID: "surface", ActorUserID: "user", Text: "/mcpoauth github"}
	first := s.ApplySurfaceAction(action)[0].Command.CommandID
	second := s.ApplySurfaceAction(action)[0].Command.CommandID
	url := agentproto.Event{Kind: agentproto.EventMCPOAuthLoginURLReady, CommandID: second, MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{ServerName: "github", AuthorizationURL: "https://auth.example/two"}}
	if out := s.ApplyAgentEvent("instance", url); len(out) != 1 {
		t.Fatalf("new URL: %#v", out)
	}
	url.Kind = agentproto.EventMCPOAuthLoginCompleted
	url.CommandID = first
	if out := s.ApplyAgentEvent("instance", url); len(out) != 0 {
		t.Fatalf("old completion: %#v", out)
	}
	url.CommandID = second
	if out := s.ApplyAgentEvent("instance", url); len(out) != 1 {
		t.Fatalf("new completion: %#v", out)
	}
}
