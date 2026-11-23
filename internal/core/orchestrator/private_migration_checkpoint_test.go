package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
)

func TestPrivateMigrationReleasedQueueSurvivesCheckpointRestart(t *testing.T) {
	seed := NewService(nil, Config{PrivateApplication: true}, nil)
	seed.MaterializeSurface("s", "g", "chat", "user")
	c, err := seed.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	c.NextQueueID = 1
	c.QueuedQueueItemIDs = []string{"queue-1"}
	c.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: "s", Status: state.QueueItemQueued}}
	old := NewService(nil, Config{PrivateApplication: true}, nil)
	if err := old.RestorePrivateCheckpoint(c); err != nil {
		t.Fatal(err)
	}
	if old.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "s", Kind: control.ActionTextMessage}) {
		t.Fatal("unverified queue was released")
	}
	c.MigrationRecoveryReleased = true
	restored := NewService(nil, Config{PrivateApplication: true}, nil)
	if err := restored.RestorePrivateCheckpoint(c); err != nil {
		t.Fatal(err)
	}
	if !restored.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "s", Kind: control.ActionTextMessage}) {
		t.Fatal("verified queue stayed held")
	}
	exported, err := restored.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	if !exported.MigrationRecoveryReleased {
		t.Fatal("release lost on checkpoint")
	}
	again := NewService(nil, Config{PrivateApplication: true}, nil)
	if err = again.RestorePrivateCheckpoint(exported); err != nil {
		t.Fatal(err)
	}
	if !again.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "s", Kind: control.ActionTextMessage}) {
		t.Fatal("restart reheld verified queue")
	}
}
