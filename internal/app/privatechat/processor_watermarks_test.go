package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestProcessorSeedsAllOwnerWatermarksBeforeLazyLoad(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	old := Owner{"gateway", "old-surface", "old-chat", "old-user", "tenant", "app"}
	a := New(nil, orchestrator.Config{})
	if err = a.RegisterOwner(old); err != nil {
		t.Fatal(err)
	}
	if err = a.Service().SeedPrivateIDWatermarks(orchestrator.PrivateIDWatermarks{Queue: 41, Image: 33, Request: 27, Pickers: orchestrator.PrivatePickerWatermarks{Path: 13, Target: 12, History: 11, Review: 10, Compact: 9, Plan: 8, Launcher: 7}}); err != nil {
		t.Fatal(err)
	}
	core, err := a.Service().ExportPrivateCheckpoint(old.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: old, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	fresh := New(nil, orchestrator.Config{})
	p, err := NewProcessor(fresh, j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Service().Surface(old.SurfaceID) != nil {
		t.Fatal("another owner's surface was imported")
	}
	current := Owner{"gateway", "new-surface", "new-chat", "new-user", "tenant", "app"}
	menu := privateAction(current, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	seq := admittedAction(t, j, current, "menu", "control", menu)
	if _, err = p.ApplyIngress(ctx, current, 1, seq); err != nil {
		t.Fatal(err)
	}
	if err = fresh.RegisterInstance(current.SurfaceID, &state.InstanceRecord{InstanceID: "new-instance", Online: true, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/workspace"}}}); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(current, control.ActionAttachInstance)
	attach.InstanceID = "new-instance"
	if _, err = fresh.Action(attach); err != nil {
		t.Fatal(err)
	}
	text := privateAction(current, control.ActionTextMessage)
	text.MessageID = "message"
	text.Text = "hello"
	seq = admittedAction(t, j, current, "message", "input", text)
	if _, err = p.ApplyIngress(ctx, current, 1, seq); err != nil {
		t.Fatal(err)
	}
	saved, err := j.LoadCheckpoint(ctx, current, 1)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Core.NextQueueID != 42 || saved.Core.QueueItems["queue-42"] == nil {
		t.Fatalf("queue watermark reused: %#v", saved.Core.QueueItems)
	}
	if saved.Core.NextImageID < 33 || saved.Core.NextCommandID < 27 || saved.Core.PickerWatermarks == nil || saved.Core.PickerWatermarks.Path < 13 || saved.Core.PickerWatermarks.Launcher < 7 {
		t.Fatalf("other counters not seeded: %#v", saved.Core)
	}
}

func TestJournalRejectsConcurrentProcessors(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{}); err == nil {
		t.Fatal("second live writer accepted")
	}
}
func TestProcessorRejectsCheckpointWithoutPickerWatermarks(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	a := New(nil, orchestrator.Config{})
	if err = a.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := a.Service().ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	// Simulate a checkpoint written by an earlier development schema with no
	// picker watermark: startup refuses to invent a safe high-water mark.
	if _, err = j.db.Exec(`UPDATE private_checkpoint SET data=json_remove(data,'$.PickerWatermarks')`); err != nil {
		t.Fatal(err)
	}
	if _, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{}); err == nil {
		t.Fatal("missing picker watermarks silently accepted")
	}
}
