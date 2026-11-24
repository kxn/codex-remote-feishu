package privatechat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateOwnerDeadlineWithoutNativeHelloSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	now := time.Now().UTC().Truncate(time.Second)
	j, e := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	p, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.ensureOwner(ctx, o, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = p.app.Action(privateAction(o, control.ActionStatus)); e != nil {
		t.Fatal(e)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface == nil {
		t.Fatal("missing surface")
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "unconnected-instance", Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace, PrepareNewThread: true}
	core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if e != nil {
		t.Fatal(e)
	}
	core.MigrationRecoveryReleased = true
	core.NextQueueID = 1
	core.QueuedQueueItemIDs = []string{"queue-1"}
	core.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, Status: state.QueueItemQueued}}
	if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	// Recreate the processor before expiry to prove restored checkpoints are
	// scheduled even when no native instance has ever authenticated.
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	owners, e := j.DeadlineOwnersPage(ctx, 128, 0)
	if e != nil || len(owners) != 1 || owners[0].Owner != o {
		t.Fatalf("owners=%v error=%v", len(owners), e)
	}
	effects, e := p.ApplyOwnerDeadline(ctx, o, 1, "expiry", now)
	if e != nil {
		t.Fatal(e)
	}
	kill, notice := false, false
	for _, v := range effects {
		event, _, e := DecodeStoredEffect(v.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil {
			t.Fatal("owner deadline dispatched native work")
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless {
			kill = event.DaemonCommand.InstanceID == "unconnected-instance"
		}
		if event.Notice != nil && event.Notice.Code == "headless_start_timeout" {
			notice = true
		}
	}
	if !kill || !notice {
		t.Fatalf("missing exact kill/notice: kill=%v notice=%v", kill, notice)
	}
	cp, e := j.LoadCheckpoint(ctx, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(cp.Core.QueuedQueueItemIDs) != 1 || cp.Core.QueuedQueueItemIDs[0] != "queue-1" {
		t.Fatal("queued input changed or replayed")
	}
	if _, held := p.app.Service().PrivateRecoveryCheckpoint(o.SurfaceID); held {
		t.Fatal("released queued input remained globally held")
	}
	for _, h := range cp.Core.Holds {
		if h.Kind == "headless_launch" {
			t.Fatal("expired launch remains pending")
		}
	}
	replay, e := p.ApplyOwnerDeadline(ctx, o, 1, "expiry", now)
	if e != nil || len(replay) != len(effects) {
		t.Fatalf("replay effects=%d err=%v", len(replay), e)
	}
}

func TestPrivateOwnerExitWithoutNativeHelloUsesRealProcessIdentity(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	now := time.Now().UTC().Truncate(time.Second)
	j, e := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	p, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.ensureOwner(ctx, o, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = p.app.Action(privateAction(o, control.ActionStatus)); e != nil {
		t.Fatal(e)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface == nil {
		t.Fatal("missing surface")
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "unconnected-instance", Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Purpose: state.HeadlessLaunchPurposeFreshWorkspace, PrepareNewThread: true}
	core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if e != nil {
		t.Fatal(e)
	}
	if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	// Recreate the processor before expiry to prove restored checkpoints are
	// scheduled even when no native instance has ever authenticated.
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	owners, e := j.DeadlineOwnersPage(ctx, 128, 0)
	if e != nil || len(owners) != 1 || owners[0].Owner != o {
		t.Fatalf("owners=%v error=%v", len(owners), e)
	}
	child := exec.Command("sh", "-c", "sleep 0.2")
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	identity, e := hostinventory.CaptureProcess(child.Process.Pid)
	if e != nil {
		_ = child.Wait()
		t.Fatal(e)
	}
	if e = child.Wait(); e != nil {
		t.Fatal(e)
	}
	if e = hostinventory.WaitProcessExit(ctx, identity); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(identity)
	if wrong, e := p.ApplyOwnerLaunchExit(ctx, o, 1, "newer-instance", string(encoded), now); e != nil || len(wrong) != 0 {
		t.Fatalf("wrong instance effects=%d err=%v", len(wrong), e)
	}
	effects, e := p.ApplyOwnerLaunchExit(ctx, o, 1, "unconnected-instance", string(encoded), now)
	if e != nil {
		t.Fatal(e)
	}
	kill, notice := false, false
	for _, v := range effects {
		event, _, e := DecodeStoredEffect(v.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil {
			t.Fatal("owner deadline dispatched native work")
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless {
			kill = event.DaemonCommand.InstanceID == "unconnected-instance"
		}
		if event.Notice != nil && event.Notice.Code == "workspace_create_start_failed" {
			notice = true
		}
	}
	if !kill || !notice {
		t.Fatalf("missing exact kill/notice: kill=%v notice=%v", kill, notice)
	}
	cp, e := j.LoadCheckpoint(ctx, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, h := range cp.Core.Holds {
		if h.Kind == "headless_launch" {
			t.Fatal("expired launch remains pending")
		}
	}
	replay, e := p.ApplyOwnerLaunchExit(ctx, o, 1, "unconnected-instance", string(encoded), now)
	if e != nil || len(replay) != len(effects) {
		t.Fatalf("replay effects=%d err=%v", len(replay), e)
	}
}

func TestPrivateOwnerDeadlineSameSecondDoesNotCollideAcrossOwners(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	j, e := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	p, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	owners := []Owner{{"g", "surface-one", "chat-one", "user-one", "tenant", "app"}, {"g", "surface-two", "chat-two", "user-two", "tenant", "app"}}
	for _, o := range owners {
		if _, e = p.ensureOwner(ctx, o, 1); e != nil {
			t.Fatal(e)
		}
		if _, e = p.app.Action(privateAction(o, control.ActionStatus)); e != nil {
			t.Fatal(e)
		}
		p.app.Service().Surface(o.SurfaceID).PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "instance-" + o.SurfaceID, Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace}
		cp, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			t.Fatal(e)
		}
		if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: cp}, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, o := range owners {
		effects, e := p.ApplyOwnerDeadline(ctx, o, 1, "tick:same", now)
		if e != nil || len(effects) == 0 {
			t.Fatalf("effects=%d err=%v", len(effects), e)
		}
		for _, effect := range effects {
			if seen[effect.Effect.ID] {
				t.Fatal("effect identity crossed owners")
			}
			seen[effect.Effect.ID] = true
		}
	}
}

func TestPrivateOwnerDeadlineSameSecondDifferentEpoch(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	j, e := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	var p *Processor
	seen := map[string]bool{}
	for generation := int64(1); generation <= 2; generation++ {
		if p != nil {
			p.poisoned = true
		}
		p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.ensureOwner(ctx, o, generation); e != nil {
			t.Fatal(e)
		}
		if _, e = p.app.Action(privateAction(o, control.ActionStatus)); e != nil {
			t.Fatal(e)
		}
		p.app.Service().Surface(o.SurfaceID).PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: fmt.Sprintf("instance-%d", generation), Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace}
		core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			t.Fatal(e)
		}
		if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: generation, Revision: 1, Core: core}, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
			t.Fatal(e)
		}
		p.poisoned = true
		p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
		if e != nil {
			t.Fatal(e)
		}
		effects, e := p.ApplyOwnerDeadline(ctx, o, generation, "tick:same", now)
		if e != nil || len(effects) == 0 {
			t.Fatalf("generation=%d err=%v", generation, e)
		}
		for _, effect := range effects {
			if seen[effect.Effect.ID] {
				t.Fatal("effect identity crossed epochs")
			}
			seen[effect.Effect.ID] = true
		}
	}
}

func TestPrivateOwnerDeadlineAfterDisconnectedHelloAndRestart(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	now := time.Now().UTC().Truncate(time.Second)
	j, e := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	p, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.ensureOwner(ctx, o, 1); e != nil {
		t.Fatal(e)
	}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "unconnected-instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, e := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.RegisterNativeHello(ctx, lease, nativeHello(native)); e != nil {
		t.Fatal(e)
	}
	if _, e = p.app.Action(privateAction(o, control.ActionStatus)); e != nil {
		t.Fatal(e)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface == nil {
		t.Fatal("missing surface")
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "unconnected-instance", Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace, PrepareNewThread: true}
	core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if e != nil {
		t.Fatal(e)
	}
	core.MigrationRecoveryReleased = true
	core.NextQueueID = 1
	core.QueuedQueueItemIDs = []string{"queue-1"}
	core.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, Status: state.QueueItemQueued}}
	if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	// Recreate the processor before expiry to prove restored checkpoints are
	// scheduled after the previously authenticated native connection is gone.
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	owners, e := j.DeadlineOwnersPage(ctx, 128, 0)
	if e != nil || len(owners) != 1 || owners[0].Owner != o {
		t.Fatalf("owners=%v error=%v", len(owners), e)
	}
	effects, e := p.ApplyOwnerDeadline(ctx, o, 1, "expiry", now)
	if e != nil {
		t.Fatal(e)
	}
	kill, notice := false, false
	for _, v := range effects {
		event, _, e := DecodeStoredEffect(v.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil {
			t.Fatal("owner deadline dispatched native work")
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless {
			kill = event.DaemonCommand.InstanceID == "unconnected-instance"
		}
		if event.Notice != nil && event.Notice.Code == "headless_start_timeout" {
			notice = true
		}
	}
	if !kill || !notice {
		t.Fatalf("missing exact kill/notice: kill=%v notice=%v", kill, notice)
	}
	cp, e := j.LoadCheckpoint(ctx, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(cp.Core.QueuedQueueItemIDs) != 1 || cp.Core.QueuedQueueItemIDs[0] != "queue-1" {
		t.Fatal("queued input changed or replayed")
	}
	if _, held := p.app.Service().PrivateRecoveryCheckpoint(o.SurfaceID); held {
		t.Fatal("released queued input remained globally held")
	}
	for _, h := range cp.Core.Holds {
		if h.Kind == "headless_launch" {
			t.Fatal("expired launch remains pending")
		}
	}
	replay, e := p.ApplyOwnerDeadline(ctx, o, 1, "expiry", now)
	if e != nil || len(replay) != len(effects) {
		t.Fatalf("replay effects=%d err=%v", len(replay), e)
	}
}
