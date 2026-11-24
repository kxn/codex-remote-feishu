package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestEffectIdentityIncludesFullOwnerAndGeneration(t *testing.T) {
	j, a, _ := memoryJournalFixture(t)
	defer j.Close()
	b := a
	b.SurfaceID += "-2"
	b.ChatID += "-2"
	b.UserID += "-2"
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []Owner{a, b} {
		action := privateAction(o, control.ActionShowCommandMenu)
		seq := admittedAction(t, j, o, "menu-"+o.UserID, "control", action)
		if _, err = p.ApplyIngress(context.Background(), o, 1, seq); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC()
	result, err := p.ApplyHostLifecycle(context.Background(), "tick:same-second", at, func(_ *App, o Owner) []eventcontract.Event {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "tick"}}}
	})
	if err != nil || len(result) != 2 || result[0].Effect.Effect.ID == result[1].Effect.Effect.ID {
		t.Fatalf("same tick owners: %+v %v", result, err)
	}
	event := []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: a.GatewayID, SurfaceSessionID: a.SurfaceID, Notice: &control.Notice{Text: "settings"}}}
	for _, key := range []string{"tick:42", "settings:revision-1"} {
		first, e := p.freezeEffects(a, 1, key, event)
		if e != nil {
			t.Fatal(e)
		}
		second, e := p.freezeEffects(b, 1, key, event)
		if e != nil {
			t.Fatal(e)
		}
		next, e := p.freezeEffects(a, 2, key, event)
		if e != nil {
			t.Fatal(e)
		}
		if first[0].ID == second[0].ID || first[0].ID == next[0].ID || second[0].ID == next[0].ID {
			t.Fatalf("collision for %s", key)
		}
	}
}

func TestLegacyEffectReplayKeepsStoredID(t *testing.T) {
	j, o, _ := memoryJournalFixture(t)
	defer j.Close()
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	action := privateAction(o, control.ActionShowCommandMenu)
	seq := admittedAction(t, j, o, "menu", "control", action)
	if _, err = p.ApplyIngress(context.Background(), o, 1, seq); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	calls := 0
	reducer := func(_ *App, o Owner) []eventcontract.Event {
		calls++
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "notice"}}}
	}
	result, err := p.ApplyHostLifecycle(context.Background(), "same", at, reducer)
	if err != nil || len(result) != 1 {
		t.Fatalf("first %v %v", result, err)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", o.TenantID, o.AppID, "host-lifecycle:same", 0)))
	legacy := "private-effect:" + hex.EncodeToString(sum[:])
	if _, err = j.db.Exec(`UPDATE private_effect SET id=? WHERE id=?`, legacy, result[0].Effect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	p.poisoned = true
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ensureOwner(context.Background(), o, 1); err != nil {
		t.Fatal(err)
	}
	replay, err := p.ApplyHostLifecycle(context.Background(), "same", at, reducer)
	if err != nil || len(replay) != 1 || replay[0].Effect.Effect.ID != legacy || calls != 1 {
		t.Fatalf("legacy replay %v %v calls=%d", replay, err, calls)
	}
}

func TestTwoOwnersSameDeadlinePersistDistinctEffects(t *testing.T) {
	ctx := context.Background()
	j, a, _ := memoryJournalFixture(t)
	defer j.Close()
	b := a
	b.SurfaceID += "-2"
	b.ChatID += "-2"
	b.UserID += "-2"
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for i, o := range []Owner{a, b} {
		if _, err = p.ensureOwner(ctx, o, 1); err != nil {
			t.Fatal(err)
		}
		if _, err = p.app.Action(privateAction(o, control.ActionStatus)); err != nil {
			t.Fatal(err)
		}
		v := p.app.Service().Surface(o.SurfaceID)
		v.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: fmt.Sprintf("instance-%d", i), Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace, PrepareNewThread: true}
		core, e := p.app.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			t.Fatal(e)
		}
		revision := p.owners[o].revision
		if e = j.commitCheckpointExtended(ctx, Checkpoint{Owner: o, Generation: 1, Revision: revision + 1, Core: core}, revision, nil, 0, nil, nil, nil, nil, nil, nil, nil); e != nil {
			t.Fatal(e)
		}
	}
	p.poisoned = true
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	x, err := p.ApplyOwnerDeadline(ctx, a, 1, "same-second", now)
	if err != nil || len(x) == 0 {
		t.Fatalf("first deadline %v %v", x, err)
	}
	y, err := p.ApplyOwnerDeadline(ctx, b, 1, "same-second", now)
	if err != nil || len(y) == 0 {
		t.Fatalf("second deadline %v %v", y, err)
	}
	seen := map[string]bool{}
	for _, e := range x {
		seen[e.Effect.ID] = true
	}
	for _, e := range y {
		if seen[e.Effect.ID] {
			t.Fatalf("deadline effect collision %s", e.Effect.ID)
		}
	}
}

func TestTwoOwnersSameSettingsRevisionPersistDistinctEffects(t *testing.T) {
	ctx := context.Background()
	j, a, _ := memoryJournalFixture(t)
	defer j.Close()
	b := a
	b.SurfaceID += "-2"
	b.ChatID += "-2"
	b.UserID += "-2"
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(f func() error) error { return f() }
	var ids []string
	for i, o := range []Owner{a, b} {
		if _, err = p.ensureOwner(ctx, o, 1); err != nil {
			t.Fatal(err)
		}
		if _, err = p.app.Action(privateAction(o, control.ActionStatus)); err != nil {
			t.Fatal(err)
		}
		n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: fmt.Sprintf("owner-%d", i), Epoch: 1, CarrierIncarnation: fmt.Sprintf("inc-%d", i), InstanceID: fmt.Sprintf("instance-%d", i), HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
		l, e := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
		if e != nil {
			t.Fatal(e)
		}
		hello := nativeHello(n)
		hello.Instance.WorkspaceRoot = fmt.Sprintf("/tmp/workspace-%d", i)
		hello.Instance.WorkspaceKey = hello.Instance.WorkspaceRoot
		if e = p.RegisterNativeHello(ctx, l, hello); e != nil {
			t.Fatal(e)
		}
		action := privateAction(o, control.ActionAttachInstance)
		action.InstanceID = n.InstanceID
		if _, e = p.app.Action(action); e != nil {
			t.Fatal(e)
		}
		v := p.app.Service().Surface(o.SurfaceID)
		v.Backend = agentproto.BackendClaude
		_, e = j.db.Exec(`INSERT INTO private_settings_reconcile(version,tenant,app_id,gateway,surface,chat,user_id,generation) VALUES(?,?,?,?,?,?,?,1)`, "shared-revision", o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
		if e != nil {
			t.Fatal(e)
		}
		effects, e := p.ApplySettingsReconcile(ctx, o, 1, "shared-revision", "shared-revision", func() bool { return true }, guard)
		if e != nil || len(effects) == 0 {
			t.Fatalf("owner %d settings effects=%d err=%v", i, len(effects), e)
		}
		ids = append(ids, effects[0].Effect.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("shared settings generated the same effect ID")
	}
}

func TestTwoOwnersSameNativeTickPersistDistinctEffects(t *testing.T) {
	ctx := context.Background()
	j, a, _ := memoryJournalFixture(t)
	defer j.Close()
	b := a
	b.SurfaceID += "-2"
	b.ChatID += "-2"
	b.UserID += "-2"
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	seen := map[string]bool{}
	for i, o := range []Owner{a, b} {
		if _, err = p.ensureOwner(ctx, o, 1); err != nil {
			t.Fatal(err)
		}
		if _, err = p.app.Action(privateAction(o, control.ActionStatus)); err != nil {
			t.Fatal(err)
		}
		n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: fmt.Sprintf("owner-%d", i), Epoch: 1, CarrierIncarnation: fmt.Sprintf("inc-%d", i), InstanceID: fmt.Sprintf("instance-%d", i), HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
		l, e := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
		if e != nil {
			t.Fatal(e)
		}
		if e = p.RegisterNativeHello(ctx, l, nativeHello(n)); e != nil {
			t.Fatal(e)
		}
		v := p.app.Service().Surface(o.SurfaceID)
		v.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: n.InstanceID, Backend: agentproto.BackendCodex, WorkspaceKey: "/tmp/work", ThreadCWD: "/tmp/work", RequestedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Second), Purpose: state.HeadlessLaunchPurposeFreshWorkspace, PrepareNewThread: true}
		effects, e := p.ApplyPrivateTick(ctx, l, "same-second", now)
		if e != nil || len(effects) == 0 {
			t.Fatalf("owner %d tick effects=%d err=%v", i, len(effects), e)
		}
		for _, item := range effects {
			if seen[item.Effect.ID] {
				t.Fatalf("native tick effect collision %s", item.Effect.ID)
			}
			seen[item.Effect.ID] = true
		}
	}
}
