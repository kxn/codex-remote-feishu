package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestMCPOAuthDaemonCommandDispatchesAgentCommandAndStoresPending(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	prepareMCPOAuthAttachedSurface(t, app, "surface-1")
	var sent []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected instance id: %s", instanceID)
		}
		sent = append(sent, command)
		return nil
	}

	events := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth docs",
		SourceMessageID:  "msg-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "mcp_oauth_requested" {
		t.Fatalf("expected requested notice, got %#v", events)
	}
	if len(sent) != 1 {
		t.Fatalf("expected one command, got %#v", sent)
	}
	command := sent[0]
	if command.Kind != agentproto.CommandMCPOAuthLogin || command.MCP.OAuthLogin == nil {
		t.Fatalf("unexpected command: %#v", command)
	}
	if command.MCP.OAuthLogin.ServerName != "docs" || command.MCP.OAuthLogin.ThreadID != "" {
		t.Fatalf("unexpected oauth payload: %#v", command.MCP.OAuthLogin)
	}
	if pending, ok := app.pendingMCPOAuthLogins[command.CommandID]; !ok || pending.SurfaceSessionID != "surface-1" || pending.ServerName != "docs" {
		t.Fatalf("expected pending oauth command, got %#v", app.pendingMCPOAuthLogins)
	}
}

func TestMCPOAuthDaemonCommandPassesSelectedThreadWhenPresent(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	prepareMCPOAuthAttachedSurface(t, app, "surface-1")
	app.service.Surface("surface-1").SelectedThreadID = "thread-1"
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	events := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth docs",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "mcp_oauth_requested" {
		t.Fatalf("expected requested notice, got %#v", events)
	}
	if len(sent) != 1 || sent[0].MCP.OAuthLogin == nil || sent[0].MCP.OAuthLogin.ThreadID != "thread-1" {
		t.Fatalf("expected selected thread id to be passed, got %#v", sent)
	}
}

func TestMCPOAuthDaemonCommandValidationNotices(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	usage := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth",
	})
	if len(usage) != 1 || usage[0].Notice == nil || usage[0].Notice.Code != "mcp_oauth_usage" {
		t.Fatalf("expected usage notice, got %#v", usage)
	}
	detached := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth docs",
	})
	if len(detached) != 1 || detached[0].Notice == nil || detached[0].Notice.Code != "not_attached" {
		t.Fatalf("expected not attached notice, got %#v", detached)
	}
}

func TestMCPOAuthDispatchFailureDoesNotStorePending(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	prepareMCPOAuthAttachedSurface(t, app, "surface-1")
	app.sendAgentCommand = func(string, agentproto.Command) error {
		return errors.New("relay unavailable")
	}

	events := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth docs",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "mcp_oauth_dispatch_failed" {
		t.Fatalf("expected dispatch failure notice, got %#v", events)
	}
	if len(app.pendingMCPOAuthLogins) != 0 {
		t.Fatalf("expected no pending oauth commands, got %#v", app.pendingMCPOAuthLogins)
	}
}

func TestMCPOAuthPendingExistsDuringRelaySend(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	prepareMCPOAuthAttachedSurface(t, app, "surface-1")
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		app.mu.Lock()
		_, pending := app.pendingMCPOAuthLogins[command.CommandID]
		app.mu.Unlock()
		if !pending {
			t.Fatalf("expected pending oauth command during send for %s", command.CommandID)
		}
		return nil
	}

	events := app.handleMCPOAuthLoginDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandMCPOAuthLogin,
		SurfaceSessionID: "surface-1",
		Text:             "/mcpoauth docs",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "mcp_oauth_requested" {
		t.Fatalf("expected requested notice, got %#v", events)
	}
}

func TestMCPOAuthAckRejectClearsPending(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.pendingMCPOAuthLogins["cmd-oauth-1"] = pendingMCPOAuthLogin{
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-1",
		ServerName:       "docs",
	}
	events, handled := app.handleMCPOAuthLoginCommandAckLocked("inst-1", agentproto.CommandAck{
		CommandID: "cmd-oauth-1",
		Accepted:  false,
		Error:     "unsupported",
	})
	if !handled {
		t.Fatalf("expected oauth ack to be handled")
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "mcp_oauth_rejected" {
		t.Fatalf("expected rejected notice, got %#v", events)
	}
	if len(app.pendingMCPOAuthLogins) != 0 {
		t.Fatalf("expected pending oauth command to clear, got %#v", app.pendingMCPOAuthLogins)
	}
}

func TestMCPOAuthSystemErrorClearsPending(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.pendingMCPOAuthLogins["cmd-oauth-1"] = pendingMCPOAuthLogin{
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-1",
		ServerName:       "docs",
	}

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{
		agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
			Code:      "mcp_oauth_login_failed",
			CommandID: "cmd-oauth-1",
			Message:   "Codex 拒绝了这次 MCP 服务认证请求。",
			Details:   "OAuth login is only supported for streamable HTTP servers.",
		}),
	})
	if len(app.pendingMCPOAuthLogins) != 0 {
		t.Fatalf("expected pending oauth command to clear after system error, got %#v", app.pendingMCPOAuthLogins)
	}
	if len(gateway.operations) == 0 {
		t.Fatalf("expected failure notice to be sent")
	}
}

func TestMCPOAuthEventsRouteToPendingSurface(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.pendingMCPOAuthLogins["cmd-oauth-1"] = pendingMCPOAuthLogin{
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-1",
		ServerName:       "docs",
	}

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventMCPOAuthLoginURLReady,
		CommandID: "cmd-oauth-1",
		MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{
			ServerName:       "docs",
			AuthorizationURL: "https://auth.example/consent",
		},
	}})
	if len(app.pendingMCPOAuthLogins) != 1 {
		t.Fatalf("expected pending oauth command to stay until completion, got %#v", app.pendingMCPOAuthLogins)
	}
	if len(gateway.operations) == 0 {
		t.Fatalf("expected URL ready notice to be sent")
	}

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventMCPOAuthLoginCompleted,
		CommandID: "cmd-oauth-1",
		MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{
			ServerName: "docs",
			Success:    true,
		},
	}})
	if len(app.pendingMCPOAuthLogins) != 0 {
		t.Fatalf("expected pending oauth command to clear after completion, got %#v", app.pendingMCPOAuthLogins)
	}
}

func TestMCPOAuthCompletionWithoutPendingDoesNotBroadcast(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventMCPOAuthLoginCompleted,
		CommandID: "cmd-unknown",
		MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{
			ServerName: "docs",
			Success:    true,
		},
	}})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no broadcast for uncorrelated completion, got %#v", gateway.operations)
	}
}

func prepareMCPOAuthAttachedSurface(t *testing.T, app *App, surfaceID string) {
	t.Helper()
	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
		},
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-1",
			Name:     "修复登录",
			CWD:      "/data/dl/droid",
			Loaded:   true,
		}},
	}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := app.service.Surface(surfaceID)
	if surface == nil || surface.AttachedInstanceID != "inst-1" {
		t.Fatalf("expected attached surface, got %#v", surface)
	}
}
