package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateFrozenInputsUseActualReactionSelection(t *testing.T) {
	svc, surface := applauseFixture(t)
	svc.config.PrivateApplication = true
	svc.Instance("inst-1").CapabilitiesDeclared = false
	svc.Instance("inst-1").Capabilities = agentproto.Capabilities{}
	svc.Instance("inst-1").ActiveThreadID = ""
	svc.Instance("inst-1").ActiveTurnID = ""
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, GatewayID: "gateway-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, GatewayID: "gateway-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	// Start a real core-owned pending turn. Merely setting the instance's
	// reported ActiveTurnID does not create the core's remote binding.
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, GatewayID: "gateway-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", MessageID: "running", Text: "already running"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
	for _, source := range []string{"first", "selected"} {
		if source == "selected" {
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, GatewayID: "gateway-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", MessageID: "image", LocalPath: "/tmp/reference.png", MIMEType: "image/png"})
		}
		svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, GatewayID: "gateway-1", SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", MessageID: source, Text: source})
	}
	if len(surface.QueuedQueueItemIDs) != 2 {
		t.Fatalf("expected the old busy queue, got %v", surface.QueuedQueueItemIDs)
	}
	selected := surface.QueuedQueueItemIDs[1]
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionReactionCreated, SurfaceSessionID: "surface-1", TargetMessageID: "selected", ReactionType: "THUMBSUP"})
	var command *agentproto.Command
	for _, e := range events {
		if e.Command != nil && e.Command.Kind == agentproto.CommandTurnSteer {
			command = e.Command
		}
	}
	if command == nil {
		t.Fatal("old core did not select a steer command")
	}
	if command.CommandID == "" {
		command.CommandID = "frozen-steer"
	}
	svc.BindPendingRemoteCommand("surface-1", command.CommandID)
	manifest, err := svc.FrozenCommandInputs("surface-1", command.CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest == nil || len(manifest.Items) != 1 || manifest.Items[0].QueueItemID != selected || len(manifest.Items[0].SourceMessageIDs) != 2 || manifest.Items[0].SourceMessageIDs[0] != "selected" || manifest.Items[0].SourceMessageIDs[1] != "image" || manifest.Items[0].SourceKind != state.QueueItemSourceUser {
		t.Fatalf("manifest changed the selected input: %#v", manifest)
	}
	if len(surface.QueuedQueueItemIDs) != 1 || surface.QueueItems[surface.QueuedQueueItemIDs[0]].SourceMessageID != "first" {
		t.Fatal("selection consumed the earlier input")
	}
	manifest.Items[0].SourceMessageIDs[0] = "changed-by-caller"
	again, err := svc.FrozenCommandInputs("surface-1", command.CommandID)
	if err != nil || again.Items[0].SourceMessageIDs[0] != "selected" {
		t.Fatalf("manifest aliases live state: %#v %v", again, err)
	}
	svc.MaterializeSurface("other", "gateway-1", "other-chat", "user-2")
	if _, err = svc.FrozenCommandInputs("other", command.CommandID); err == nil {
		t.Fatal("foreign surface acquired a command's input manifest")
	}
	if manifest, err = svc.FrozenCommandInputs("surface-1", "catalog-read"); err != nil || manifest != nil {
		t.Fatalf("invented input for a read: %#v %v", manifest, err)
	}
}

func TestPrivateFrozenInputsKeepAutomaticSourceDistinct(t *testing.T) {
	svc, surface := applauseFixture(t)
	svc.config.PrivateApplication = true
	item := &state.QueueItemRecord{ID: "queue-1", SurfaceSessionID: surface.SurfaceSessionID, SourceKind: state.QueueItemSourceAutoContinue, AutoContinueEpisodeID: "episode-1", Status: state.QueueItemQueued}
	surface.QueueItems[item.ID] = item
	svc.turns.bindPendingRemote("inst-1", &remoteTurnBinding{InstanceID: "inst-1", SurfaceSessionID: surface.SurfaceSessionID, QueueItemID: item.ID, CommandID: "auto-command", ThreadID: "thread-1"})
	manifest, err := svc.FrozenCommandInputs(surface.SurfaceSessionID, "auto-command")
	if err != nil || manifest == nil || len(manifest.Items) != 1 {
		t.Fatalf("automatic manifest: %#v %v", manifest, err)
	}
	frozen := manifest.Items[0]
	if frozen.SourceKind != state.QueueItemSourceAutoContinue || frozen.AutoContinueEpisodeID != "episode-1" || len(frozen.SourceMessageIDs) != 0 {
		t.Fatalf("invented SDK provenance for continuation: %#v", frozen)
	}
}
