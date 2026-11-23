package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateCheckpointRestoresTargetPickerOptionsForItsOwner(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewService(func() time.Time { return now }, Config{PrivateApplication: true}, nil)
	s.MaterializeSurface("s", "g", "chat", "user")
	v := s.root.Surfaces["s"]
	s.setActiveOwnerCardFlow(v, &activeOwnerCardFlowRecord{FlowID: "flow", Kind: ownerCardFlowKindTargetPicker, OwnerUserID: "user", MessageID: "card", ExpiresAt: now.Add(time.Hour)})
	s.setActiveTargetPicker(v, &activeTargetPickerRecord{PickerID: "picker", OwnerUserID: "user", ExpiresAt: now.Add(time.Hour), OptionSnapshot: &targetPickerOptionSnapshot{Workspaces: []workspaceSelectionEntry{{workspaceKey: "/work", label: "project", attachable: true}}, Sessions: map[string][]control.FeishuTargetPickerSessionOption{"/work": {}}, DefaultSessions: map[string]string{"/work": "new"}, CurrentWorkspaceKey: "/work"}})
	c, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var decoded PrivateCheckpoint
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	other := NewService(func() time.Time { return now }, Config{PrivateApplication: true}, nil)
	if err := other.RestorePrivateCheckpoint(decoded); err != nil {
		t.Fatal(err)
	}
	picker := other.activeTargetPicker(other.root.Surfaces["s"])
	if picker == nil || picker.OptionSnapshot == nil || len(picker.OptionSnapshot.Workspaces) != 1 || picker.OptionSnapshot.Workspaces[0].workspaceKey != "/work" || picker.OptionSnapshot.Workspaces[0].label != "project" || !picker.OptionSnapshot.Workspaces[0].attachable {
		t.Fatalf("lost options: %#v", picker)
	}
	a := control.Action{SurfaceSessionID: "s", GatewayID: "g", ChatID: "chat", ActorUserID: "user", PickerID: "picker", Inbound: &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "card"}}
	if !other.PrivateRestoredPickerCardMatches(a) {
		t.Fatal("persisted picker card did not match")
	}
	a.ActorUserID = "other"
	if other.PrivateRestoredPickerCardMatches(a) {
		t.Fatal("other owner matched card")
	}
	decoded.UI.TargetPicker.OwnerUserID = "other"
	if err := NewService(func() time.Time { return now }, Config{PrivateApplication: true}, nil).RestorePrivateCheckpoint(decoded); err == nil {
		t.Fatal("cross-owner picker accepted")
	}
}
