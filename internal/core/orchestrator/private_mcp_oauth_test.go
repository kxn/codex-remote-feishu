package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateMCPOAuthPreservesOwnerAndCompletionAcrossRestart(t *testing.T) {
	now := time.Now().UTC()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	instance := &state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Threads: map[string]*state.ThreadRecord{}}
	s.UpsertInstance(instance)
	s.MaterializeSurface("surface", "gateway", "chat", "user")
	s.root.Surfaces["surface"].AttachedInstanceID = "instance"
	events := s.ApplySurfaceAction(control.Action{Kind: control.ActionMCPOAuthCommand, SurfaceSessionID: "surface", ActorUserID: "user", Text: "/mcpoauth github"})
	if len(events) != 1 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandMCPOAuthLogin {
		t.Fatalf("OAuth start: %#v", events)
	}
	command := events[0].Command.CommandID
	cp, err := s.ExportPrivateCheckpoint("surface")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	restored.UpsertInstance(instance)
	url := agentproto.Event{Kind: agentproto.EventMCPOAuthLoginURLReady, CommandID: command, MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{ServerName: "github", AuthorizationURL: "https://auth.example/one"}}
	if out := restored.ApplyAgentEvent("other", url); len(out) != 0 {
		t.Fatal("foreign carrier routed login")
	}
	out := restored.ApplyAgentEvent("instance", url)
	if len(out) != 1 || out[0].Notice == nil || out[0].Notice.Code != "mcp_oauth_url_ready" {
		t.Fatalf("missing login URL: %#v", out)
	}
	url.Kind = agentproto.EventMCPOAuthLoginCompleted
	url.MCPOAuthLogin.Success = true
	out = restored.ApplyAgentEvent("instance", url)
	if len(out) != 1 || out[0].Notice == nil || out[0].Notice.Code != "mcp_oauth_completed" {
		t.Fatalf("missing completion: %#v", out)
	}
	if out = restored.ApplyAgentEvent("instance", url); len(out) != 0 {
		t.Fatal("duplicate completion reappeared")
	}
}
