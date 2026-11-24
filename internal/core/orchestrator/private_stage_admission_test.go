package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateStageDraftCancelledBeforeReceipt(t *testing.T) {
	for _, kind := range []control.ActionKind{control.ActionTextMessage, control.ActionFileMessage} {
		t.Run(string(kind), func(t *testing.T) {
			s, _ := privateUpgradeResumeFixture(t, false)
			v := s.Surface("s")
			v.QueuedQueueItemIDs = nil
			v.QueueItems = map[string]*state.QueueItemRecord{}
			action := control.Action{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: "message", Text: "with attachment", LocalPath: "/tmp/source", FileName: "source"}
			id, events, err := s.BeginPrivateStage(action)
			if err != nil || id == "" {
				t.Fatalf("stage id=%q events=%v err=%v", id, events, err)
			}
			pending, err := s.PrivateStagePending("s", id, "message", "user")
			if err != nil || !pending {
				t.Fatalf("missing pending: %v %v", pending, err)
			}
			if kind == control.ActionTextMessage && (len(v.QueuedQueueItemIDs) != 1 || !v.QueueItems[id].StagePending || v.ActiveQueueItemID != "") {
				t.Fatal("stage queue dispatched early")
			}
			s.discardDrafts(v)
			events, err = s.CompletePrivateStage("s", id, "message", "user", "/tmp/copied", "source", "file prompt")
			if err != nil || len(events) != 0 || len(v.QueuedQueueItemIDs) != 0 {
				t.Fatalf("late receipt reintroduced draft: %v %v", events, err)
			}
		})
	}
}

func TestPrivateStageNewThreadCancelsQueuedDraft(t *testing.T) {
	s, _ := privateUpgradeResumeFixture(t, false)
	v := s.Surface("s")
	v.QueuedQueueItemIDs = nil
	v.QueueItems = map[string]*state.QueueItemRecord{}
	action := control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: "message", Text: "with attachment"}
	id, _, err := s.BeginPrivateStage(action)
	if err != nil || id == "" {
		t.Fatalf("stage %q: %v", id, err)
	}
	s.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user"})
	pending, err := s.PrivateStagePending("s", id, "message", "user")
	if err != nil || pending {
		t.Fatalf("/new retained old stage: %v %v", pending, err)
	}
	if events, err := s.CompletePrivateStage("s", id, "message", "user", "/tmp/copied", "source", "file prompt"); err != nil || len(events) != 0 {
		t.Fatalf("late receipt reintroduced input: %v %v", events, err)
	}
}

func TestPrivateReplacementPendingStageNewCancelsOriginalTarget(t *testing.T) {
	s, req := privateUpgradeResumeFixture(t, false)
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "old", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	actual := PrivateNativeProfiles{Codex: req.ProfileID}
	s.SetPrivateNativeProfiles("s", actual)
	s.AuthenticatePrivateNavigation("s", owner)
	cp, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r := newServiceForTest(&now)
	r.config.PrivateApplication = true
	if err = r.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	r.SetPrivateThreadPolicy("s", PrivateThreadPolicy{InstanceNamespaces: map[string]string{"old": "history"}})
	if _, err = r.MaterializeRetiredPrivateReplacement("s", owner, PrivateRetiredContract{Profiles: actual}, req.CWD); err != nil {
		t.Fatal(err)
	}
	imageAction := control.Action{Kind: control.ActionImageMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: "image", LocalPath: "/tmp/image", MIMEType: "image/png"}
	r.ApplySurfaceAction(imageAction)
	foundImage := false
	for _, image := range r.Surface("s").StagedImages {
		if image.SourceMessageID == "image" && image.State == state.ImageStaged {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatal("replacement did not stage image")
	}
	action := control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user", MessageID: "late-file", Text: "request"}
	id, _, err := r.BeginPrivateStage(action)
	if err != nil || id == "" {
		t.Fatalf("stage %q %v", id, err)
	}
	if item := r.Surface("s").QueueItems[id]; item == nil || !item.StagePending || item.FrozenDispatchPlan.ExecutionThreadID != "thread" {
		t.Fatalf("unfrozen original target: %+v", item)
	}
	r.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", ActorUserID: "user"})
	pending, err := r.PrivateStagePending("s", id, "late-file", "user")
	if err != nil || pending {
		t.Fatalf("/new failed to cancel: %v %v", pending, err)
	}
	if events, err := r.CompletePrivateStage("s", id, "late-file", "user", "/tmp/copied", "file", "file prompt"); err != nil || len(events) != 0 {
		t.Fatalf("late stage retargeted: %v %v", events, err)
	}
}
