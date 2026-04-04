package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestMenuActionKindKnownValues(t *testing.T) {
	tests := map[string]control.ActionKind{
		"list":          control.ActionListInstances,
		"status":        control.ActionStatus,
		"stop":          control.ActionStop,
		"threads":       control.ActionShowThreads,
		"sessions":      control.ActionShowThreads,
		"use":           control.ActionShowThreads,
		"show_threads":  control.ActionShowThreads,
		"show_sessions": control.ActionShowThreads,
		"useall":        control.ActionShowAllThreads,
		"threads_all":   control.ActionShowAllThreads,
	}
	for key, want := range tests {
		got, ok := menuActionKind(key)
		if !ok || got != want {
			t.Fatalf("event key %q => (%q, %v), want (%q, true)", key, got, ok, want)
		}
	}
}

func TestMenuActionReasoningPresets(t *testing.T) {
	tests := map[string]string{
		"reasonlow":    "/reasoning low",
		"reasonmedium": "/reasoning medium",
		"reasonhigh":   "/reasoning high",
		"reasonxhigh":  "/reasoning xhigh",
	}
	for key, wantText := range tests {
		got, ok := menuAction(key)
		if !ok {
			t.Fatalf("expected menu action for %q", key)
		}
		if got.Kind != control.ActionReasoningCommand || got.Text != wantText {
			t.Fatalf("event key %q => %#v, want reasoning command %q", key, got, wantText)
		}
	}
}

func TestMenuActionKindUnknownValueIsIgnored(t *testing.T) {
	got, ok := menuActionKind("unexpected")
	if ok || got != "" {
		t.Fatalf("unexpected menu action result: (%q, %v)", got, ok)
	}
}

func TestSurfaceIDForInboundUsesUserScopeForP2P(t *testing.T) {
	got := surfaceIDForInbound("oc_xxx", "p2p", "user-1")
	if got != "feishu:user:user-1" {
		t.Fatalf("unexpected p2p surface id: %q", got)
	}
}

func TestSurfaceIDForInboundUsesChatScopeForGroup(t *testing.T) {
	got := surfaceIDForInbound("oc_xxx", "group", "user-1")
	if got != "feishu:chat:oc_xxx" {
		t.Fatalf("unexpected group surface id: %q", got)
	}
}

func TestParseTextActionRecognizesModelAndReasoningCommands(t *testing.T) {
	tests := map[string]control.ActionKind{
		"/model":          control.ActionModelCommand,
		"/model gpt-5.4":  control.ActionModelCommand,
		"/reasoning high": control.ActionReasoningCommand,
		"/effort medium":  control.ActionReasoningCommand,
	}
	for input, want := range tests {
		action, handled := parseTextAction(input)
		if !handled {
			t.Fatalf("expected %q to be handled", input)
		}
		if action.Kind != want {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, want)
		}
		if action.Text != input {
			t.Fatalf("input %q => text %q, want raw command", input, action.Text)
		}
	}
}

func TestParseTextActionRecognizesSessionCommands(t *testing.T) {
	tests := map[string]control.ActionKind{
		"/threads":     control.ActionShowThreads,
		"/use":         control.ActionShowThreads,
		"/sessions":    control.ActionShowThreads,
		"/useall":      control.ActionShowAllThreads,
		"/sessionsall": control.ActionShowAllThreads,
	}
	for input, want := range tests {
		action, handled := parseTextAction(input)
		if !handled {
			t.Fatalf("expected %q to be handled", input)
		}
		if action.Kind != want {
			t.Fatalf("input %q => kind %q, want %q", input, action.Kind, want)
		}
	}
}
