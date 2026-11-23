package privatechat

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestLegacyPrivateJourney(t *testing.T) {
	app := New(func() time.Time { return time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC) }, orchestrator.Config{})
	owner := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	if err := app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("surface", &state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	action := func(kind control.ActionKind) control.Action {
		return control.Action{Kind: kind, GatewayID: "gateway", SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user"}
	}
	attach := action(control.ActionAttachInstance)
	attach.InstanceID = "instance"
	if _, err := app.Action(attach); err != nil {
		t.Fatal(err)
	}
	menu := action(control.ActionShowCommandMenu)
	menu.Text = "/menu"
	menuEvents, err := app.Action(menu)
	if err != nil || len(menuEvents) == 0 {
		t.Fatalf("menu: %v %#v", err, menuEvents)
	}
	first := action(control.ActionTextMessage)
	first.MessageID = "m1"
	first.Text = "你好"
	events, err := app.Action(first)
	if err != nil {
		t.Fatal(err)
	}
	dispatched := false
	for _, event := range events {
		if event.Command != nil {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatalf("missing native dispatch: %#v", events)
	}
	second := action(control.ActionTextMessage)
	second.MessageID = "m2"
	second.Text = "排队"
	events, err = app.Action(second)
	if err != nil || len(events) != 1 || events[0].PendingInput == nil || events[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("queue: %v %#v", err, events)
	}
	if _, err = app.AgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}}); err != nil {
		t.Fatal(err)
	}
	if _, err = app.AgentEvent("instance", agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "thread", TurnID: "turn", ItemID: "answer", ItemKind: "agent_message", Metadata: map[string]any{"text": "已完成"}}); err != nil {
		t.Fatal(err)
	}
	events, err = app.AgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread", TurnID: "turn", Status: "completed", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
	if err != nil {
		t.Fatal(err)
	}
	final := false
	for _, event := range events {
		if event.Block != nil && event.Block.Final && event.Block.Text == "已完成" {
			final = true
		}
	}
	if !final {
		t.Fatalf("missing final: %#v", events)
	}
}

func TestOwnerAndRetiredActionsRejected(t *testing.T) {
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(Owner{"g", "s", "c", "u", "tenant", "app"}); err != nil {
		t.Fatal(err)
	}
	action := control.Action{Kind: control.ActionStatus, GatewayID: "g", SurfaceSessionID: "s", ChatID: "group", ActorUserID: "u"}
	if _, err := app.Action(action); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("group: %v", err)
	}
	action.ChatID = "c"
	action.Kind = control.ActionCronCommand
	if _, err := app.Action(action); !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("cron: %v", err)
	}
	if _, err := app.AgentEvent("unregistered", agentproto.Event{}); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("instance: %v", err)
	}
}

// This reproduces the old staged image -> queued text -> reaction behavior.
func TestLegacyImageQueueReaction(t *testing.T) {
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(Owner{"g", "s", "c", "u", "tenant", "app"}); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	action := func(kind control.ActionKind) control.Action {
		return control.Action{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	}
	attach := action(control.ActionAttachInstance)
	attach.InstanceID = "i"
	if _, err := app.Action(attach); err != nil {
		t.Fatal(err)
	}
	use := action(control.ActionUseThread)
	use.ThreadID = "thread"
	if _, err := app.Action(use); err != nil {
		t.Fatal(err)
	}
	first := action(control.ActionTextMessage)
	first.MessageID = "first"
	first.Text = "开始"
	if _, err := app.Action(first); err != nil {
		t.Fatal(err)
	}
	if _, err := app.AgentEvent("i", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}}); err != nil {
		t.Fatal(err)
	}
	img := action(control.ActionImageMessage)
	img.MessageID = "image"
	img.LocalPath = "/tmp/queued.png"
	img.MIMEType = "image/png"
	if _, err := app.Action(img); err != nil {
		t.Fatal(err)
	}
	text := action(control.ActionTextMessage)
	text.MessageID = "queued"
	text.Text = "补充信息"
	if _, err := app.Action(text); err != nil {
		t.Fatal(err)
	}
	reaction := action(control.ActionReactionCreated)
	reaction.TargetMessageID = "queued"
	reaction.ReactionType = "ThumbsUp"
	events, err := app.Action(reaction)
	if err != nil || len(events) != 1 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("reaction: %v %#v", err, events)
	}
	inputs := events[0].Command.Prompt.Inputs
	if len(inputs) != 2 || inputs[0].Type != agentproto.InputLocalImage || inputs[1].Text != "补充信息" {
		t.Fatalf("image and text not bound: %#v", inputs)
	}
}

func TestLegacySessionAndMenuNavigation(t *testing.T) {
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(Owner{"g", "s", "c", "u", "tenant", "app"}); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterInstance("s", &state.InstanceRecord{InstanceID: "i", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	action := func(kind control.ActionKind) control.Action {
		return control.Action{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	}
	attach := action(control.ActionAttachInstance)
	attach.InstanceID = "i"
	if _, err := app.Action(attach); err != nil {
		t.Fatal(err)
	}
	use := action(control.ActionUseThread)
	use.ThreadID = "thread"
	if _, err := app.Action(use); err != nil {
		t.Fatal(err)
	}
	if got := app.Service().Surface("s").SelectedThreadID; got != "thread" {
		t.Fatalf("/use selected %q", got)
	}
	img := action(control.ActionImageMessage)
	img.MessageID = "image"
	img.LocalPath = "/tmp/image.png"
	img.MIMEType = "image/png"
	if _, err := app.Action(img); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Action(action(control.ActionNewThread)); err != nil {
		t.Fatal(err)
	}
	surface := app.Service().Surface("s")
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" || len(surface.StagedImages) != 0 {
		t.Fatalf("/new: %#v", surface)
	}
	menu := action(control.ActionShowCommandMenu)
	menu.Text = "/menu maintenance"
	events, err := app.Action(menu)
	if err != nil || len(events) != 1 || events[0].PageView == nil {
		t.Fatalf("submenu %v %#v", err, events)
	}
	if len(events[0].PageView.RelatedButtons) != 1 || events[0].PageView.RelatedButtons[0].CommandText != "/menu" {
		t.Fatalf("submenu back: %#v", events[0].PageView)
	}
	menu.Text = "/menu"
	events, err = app.Action(menu)
	if err != nil || len(events) != 1 || events[0].PageView == nil || len(events[0].PageView.RelatedButtons) != 0 {
		t.Fatalf("menu root %v %#v", err, events)
	}
}

func TestPrivateRetiredTakeoverBlockedAcrossActionParsers(t *testing.T) {
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(Owner{"g", "s", "c", "u", "tenant", "app"}); err != nil {
		t.Fatal(err)
	}
	slash, ok := control.ParseFeishuTextActionWithoutCatalog("/mode vscode")
	if !ok {
		t.Fatal("slash parser did not recognize mode")
	}
	menu, ok := control.ParseFeishuMenuActionWithoutCatalog("mode_vscode")
	if !ok {
		t.Fatal("menu parser did not recognize mode")
	}
	cases := []struct {
		name   string
		action control.Action
	}{
		{"direct", control.Action{Kind: control.ActionModeCommand, Text: "/mode vscode"}},
		{"slash", slash}, {"menu", menu},
		{"card option", control.Action{Kind: control.ActionModeCommand, Text: "/mode", OptionID: "vscode"}},
		{"follow", control.Action{Kind: control.ActionFollowLocal}},
		{"takeover migration", control.Action{Kind: control.ActionVSCodeMigrate}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.action
			a.GatewayID = "g"
			a.SurfaceSessionID = "s"
			a.ChatID = "c"
			a.ActorUserID = "u"
			if _, err := app.Action(a); !errors.Is(err, ErrUnsupportedAction) {
				t.Fatalf("retired action accepted: %#v error=%v", a, err)
			}
			if got := app.Service().Surface("s").ProductMode; got == state.ProductModeVSCode {
				t.Fatal("entered VS Code mode")
			}
		})
	}
	for _, kind := range []control.ActionKind{control.ActionNewThread, control.ActionShowThreads, control.ActionShowHistory, control.ActionAutoWhipCommand, control.ActionAutoContinueCommand} {
		a := control.Action{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
		if err := app.ValidateAction(a); err != nil {
			t.Fatalf("supported action %s rejected: %v", kind, err)
		}
	}
}
