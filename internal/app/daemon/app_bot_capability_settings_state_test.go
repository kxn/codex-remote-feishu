package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestConfigureBotCapabilitySettingsStateMaterializesStore(t *testing.T) {
	stateDir := t.TempDir()
	store, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Put(state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureBotCapabilitySettingsStateLocked(stateDir)
	app.service.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:chat:oc_room",
		"app-1",
		"oc_room",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"team-proxy",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	app.mu.Unlock()

	if got := app.service.SurfaceBackend("feishu:app-1:chat:oc_room"); got != agentproto.BackendClaude {
		t.Fatalf("SurfaceBackend = %s, want claude", got)
	}
}

func TestSyncBotCapabilitySettingsStatePersistsPrivateCommand(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})

	app.mu.Lock()
	app.configureBotCapabilitySettingsStateLocked(stateDir)
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/mode claude",
	})
	app.syncBotCapabilitySettingsStateLocked()
	app.mu.Unlock()

	reloaded, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	record, ok := reloaded.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok {
		t.Fatalf("expected persisted bot capability settings")
	}
	if record.Backend != agentproto.BackendClaude {
		t.Fatalf("persisted backend = %s, want claude", record.Backend)
	}
}

func TestBotCapabilitySettingsDaemonRoundTripPreservesInactiveSelections(t *testing.T) {
	stateDir := t.TempDir()
	store, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Put(state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		CodexProfileID:  "team-proxy",
		ClaudeProfileID: "devseek",
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureBotCapabilitySettingsStateLocked(stateDir)
	app.syncBotCapabilitySettingsStateLocked()
	app.mu.Unlock()

	reloaded, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	record, ok := reloaded.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok {
		t.Fatal("expected persisted bot capability settings")
	}
	if record.Backend != agentproto.BackendClaude || record.CodexProfileID != "team-proxy" || record.ClaudeProfileID != "devseek" {
		t.Fatalf("round-trip bot capability selections = %#v", record)
	}
}

func TestRequestResolvedPersistsBotCapabilityLifecycleMutation(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})

	app.mu.Lock()
	app.configureBotCapabilitySettingsStateLocked(stateDir)
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-1",
		DisplayName:     "droid",
		WorkspaceRoot:   "/data/dl/droid",
		WorkspaceKey:    "/data/dl/droid",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	surfaceID := "feishu:app-1:user:ou_user"
	baseAction := control.Action{
		SurfaceSessionID: surfaceID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
	}
	modeAction := baseAction
	modeAction.Kind = control.ActionModeCommand
	modeAction.Text = "/mode claude"
	app.service.ApplySurfaceAction(modeAction)
	attachAction := baseAction
	attachAction.Kind = control.ActionAttachInstance
	attachAction.InstanceID = "inst-1"
	app.service.ApplySurfaceAction(attachAction)
	useAction := baseAction
	useAction.Kind = control.ActionUseThread
	useAction.ThreadID = "thread-1"
	app.service.ApplySurfaceAction(useAction)
	planAction := baseAction
	planAction.Kind = control.ActionPlanCommand
	planAction.Text = "/plan on"
	app.service.ApplySurfaceAction(planAction)
	app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "tool/ExitPlanMode",
		},
	})
	app.syncBotCapabilitySettingsStateLocked()
	app.mu.Unlock()

	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata:  map[string]any{"decision": "accept"},
	}})

	reloaded, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	record, ok := reloaded.Get(state.BotCapabilitySettingsKey("app-1"))
	if !ok {
		t.Fatal("expected persisted bot capability settings")
	}
	if record.PlanMode != state.PlanModeSettingOff || record.PlanModeOverrideSet {
		t.Fatalf("persisted plan override = %s/%v, want off/false", record.PlanMode, record.PlanModeOverrideSet)
	}
}
