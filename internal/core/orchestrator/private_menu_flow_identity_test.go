package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateMenuNewEntryOwnsFreshFlowAndCallbackReusesIt(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user"})
	surface := s.root.Surfaces["surface"]
	intent := control.FeishuUIIntent{Kind: control.FeishuUIIntentShowCommandMenu, RawText: "/menu"}
	first := s.applyFeishuUIIntent(surface, control.Action{}, intent)[0]
	if first.PageView == nil || first.PageView.TrackingKey == "" {
		t.Fatal("first menu lacks flow")
	}
	intent.SourceMessageID = "old-card"
	back := s.applyFeishuUIIntent(surface, control.Action{Inbound: &control.ActionInboundMeta{CardCallback: true}}, intent)[0]
	if back.PageView == nil || back.PageView.TrackingKey != first.PageView.TrackingKey {
		t.Fatalf("same card back changed flow: %+v", back.PageView)
	}
	intent.SourceMessageID = ""
	fresh := s.applyFeishuUIIntent(surface, control.Action{}, intent)[0]
	if fresh.PageView == nil || fresh.PageView.TrackingKey == first.PageView.TrackingKey {
		t.Fatalf("new menu reused previous flow: %+v", fresh.PageView)
	}
}

func TestPrivateStandaloneSettingsDoNotInheritMenuFlow(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user"})
	surface := s.root.Surfaces["surface"]
	first := s.menuPageEvent(surface, "/menu", "", true)
	old := first.PageView.TrackingKey
	action := control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface", Text: "/model", Inbound: &control.ActionInboundMeta{CardCallback: true}}
	s.openConfigCommandPageForAction(surface, action)
	if flow := s.activeCommandLauncherFlow(surface); flow == nil || flow.FlowID != old {
		t.Fatal("same-card settings lost its launcher flow")
	}
	action.Inbound = nil
	events := s.openConfigCommandPageForAction(surface, action)
	if flow := s.activeCommandLauncherFlow(surface); flow != nil {
		t.Fatal("standalone settings inherited prior launcher flow")
	}
	if len(events) == 0 || events[0].PageView == nil || events[0].PageView.TrackingKey == old {
		t.Fatalf("standalone settings retained old publication scope: %+v", events)
	}
}
