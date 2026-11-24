package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateModelWaitProjectsOnlyCurrentPage(t *testing.T) {
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	a := control.Action{Kind: control.ActionModelCommand, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u", Text: "/model"}
	events := s.ApplySurfaceAction(a)
	if len(events) == 0 || events[0].PageView == nil {
		t.Fatalf("model page: %+v", events)
	}
	tracking := events[0].PageView.TrackingKey
	if tracking == "" {
		t.Fatal("missing tracking")
	}
	s.BindPrivateModelQuery("s", "query")
	catalog := &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{Model: "fixture"}}}
	updates, changed := s.ProjectPrivateModelCatalog("s", "query", tracking, "hash", "profile", "fingerprint", "namespace", 1, catalog, "")
	if !changed || len(updates) != 1 || updates[0].PageView == nil || !updates[0].PageView.PrivateModelResult {
		t.Fatalf("result: %+v %v", updates, changed)
	}
	if _, changed = s.ProjectPrivateModelCatalog("s", "query", tracking, "hash", "profile", "fingerprint", "namespace", 1, catalog, ""); changed {
		t.Fatal("duplicate observation projected twice")
	}
	if len(updates[0].PageView.RelatedButtons) == 0 {
		t.Fatal("shared result lost refresh action")
	}
	errors, changed := s.ProjectPrivateModelCatalog("s", "query", tracking, "resolution-error", "profile", "fingerprint", "", 0, nil, "profile unavailable")
	if !changed || len(errors) != 1 || errors[0].PageView == nil || len(errors[0].PageView.RelatedButtons) == 0 {
		t.Fatal("resolution error lost or refresh action missing")
	}
	if _, changed = s.ProjectPrivateModelCatalog("s", "query", tracking, "stale", "profile", "fingerprint", "namespace", 0, catalog, ""); changed {
		t.Fatal("old catalog revision accepted after error")
	}
	s.ApplySurfaceAction(control.Action{Kind: control.ActionShowCommandMenu, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"})
	if _, changed = s.ProjectPrivateModelCatalog("s", "query", tracking, "new", "profile", "fingerprint", "namespace", 2, catalog, ""); changed {
		t.Fatal("left page accepted late result")
	}
}
