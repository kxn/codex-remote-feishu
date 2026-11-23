package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateUpgradeTrackUsesDurableSharedInstallOwner(t *testing.T) {
	host, path := newUpgradeTestApp(t, &lifecycleGateway{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	tracks := install.CurrentBuildAllowedReleaseTracks()
	if len(tracks) < 2 {
		t.Skip("build policy exposes only one release track")
	}
	target := tracks[1]
	action := control.Action{Kind: control.ActionUpgradeCommand, Text: "/upgrade track " + string(target), GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "menu-card", LocalPageAction: true, Inbound: &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "menu-card", CardDaemonLifecycleID: host.daemonLifecycleID}}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "upgrade-track", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatalf("admission: %#v %v", effects, err)
	}
	before, err := install.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := &privateWorkRunner{host: host, processor: p, journal: j}
	work := privatechat.PendingEffectWork{Owner: o, Generation: 1, Effect: effects[0]}
	if err = runner.runEffect(t.Context(), work); err != nil {
		t.Fatalf("runner track result: %v", err)
	}
	after, err := install.LoadState(path)
	if err != nil || after.CurrentTrack != target {
		t.Fatalf("install track: before=%v after=%v err=%v", before.CurrentTrack, after.CurrentTrack, err)
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateExpiredUpgradeCardProducesDurableNotice(t *testing.T) {
	host, _ := newUpgradeTestApp(t, &lifecycleGateway{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	action := control.Action{Kind: control.ActionUpgradeOwnerFlow, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "old-card", OwnerFlow: &control.ActionOwnerCardFlow{FlowID: "expired-flow", OptionID: upgradeOwnerActionConfirm}}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "expired-card", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 1 {
		t.Fatalf("old callback effects: %+v", effects)
	}
	out, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("notice effects: %+v", out)
	}
	event, err := privatechat.DecodeEffectEvent(out[0].Effect.Body)
	if err != nil || event.Notice == nil || event.Notice.Code != "upgrade_owner_expired" {
		t.Fatalf("expired notice: %+v %v", event, err)
	}
	again, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID)
	if err != nil || len(again) != 1 || again[0].Effect.ID != out[0].Effect.ID {
		t.Fatalf("callback replay did not return same notice effect: %v", err)
	}
}

func TestPrivateUpgradeCheckFreezesCandidateBeforeOwnerCard(t *testing.T) {
	host, _ := newUpgradeTestApp(t, &lifecycleGateway{})
	calls := 0
	host.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		calls++
		return install.ReleaseInfo{TagName: "v2.0.0"}, nil
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	action := control.Action{Kind: control.ActionUpgradeCommand, Text: "/upgrade latest", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "msg"}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "upgrade-latest", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatalf("admit: %#v %v", effects, err)
	}
	if calls != 0 {
		t.Fatal("lookup ran before durable command")
	}
	out, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID)
	if err != nil || len(out) != 1 {
		t.Fatalf("check: %#v %v", out, err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	if flow == nil || flow.TargetVersion != "v2.0.0" || flow.StateFingerprint == "" {
		t.Fatalf("candidate: %#v", flow)
	}
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Revision == 0 {
		t.Fatalf("checkpoint: %v", err)
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID); err != nil || calls != 1 {
		t.Fatalf("check replay: %d %v", calls, err)
	}
	confirm := control.Action{Kind: control.ActionUpgradeOwnerFlow, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "card", OwnerFlow: &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionConfirm}}
	body, _ := json.Marshal(confirm)
	next, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "upgrade-confirm", Lane: "control", Payload: body})
	if err != nil {
		t.Fatal(err)
	}
	confirmEffects, err := p.ApplyIngress(t.Context(), o, 1, next)
	if err != nil || len(confirmEffects) != 1 {
		t.Fatalf("confirm: %#v %v", confirmEffects, err)
	}
	running, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, confirmEffects[0].Effect.ID)
	if err != nil || len(running) != 2 {
		t.Fatalf("running checkpoint: %#v %v", running, err)
	}
	if app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "running" {
		t.Fatal("running intent not persisted")
	}
	originalEnsure := privateEnsureReleaseBinary
	defer func() { privateEnsureReleaseBinary = originalEnsure }()
	launchCalls := 0
	privateEnsureReleaseBinary = func(context.Context, install.ReleaseBinaryOptions) (string, error) {
		launchCalls++
		return "", fmt.Errorf("controlled download failure")
	}
	// The human confirmation is already committed. An unknown progress-card
	// update must not become an execution gate for that authorized intent.
	gateway := &privatePublishGateway{fail: true}
	host.gateway = gateway
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); !errors.Is(err, errPrivatePublicationUnknown) {
		t.Fatalf("progress uncertainty: %v", err)
	}
	failed, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, running[1].Effect.ID)
	if err != nil || len(failed) != 1 || launchCalls != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "failed" {
		t.Fatalf("failed execute: %#v calls=%d err=%v", failed, launchCalls, err)
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, running[1].Effect.ID); err != nil || launchCalls != 1 {
		t.Fatalf("execute replay: %d %v", launchCalls, err)
	}
}

func TestPrivateUpgradeUsesSharedInstallJournalAndStartsHelperOnce(t *testing.T) {
	host, path := newUpgradeTestApp(t, &lifecycleGateway{})
	stateValue, err := install.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(stateValue.CurrentBinaryPath, []byte("current binary"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err = os.WriteFile(target, []byte("new binary"), 0700); err != nil {
		t.Fatal(err)
	}
	originalEnsure, originalStart := privateEnsureReleaseBinary, privateStartUpgradeHelper
	defer func() { privateEnsureReleaseBinary = originalEnsure; privateStartUpgradeHelper = originalStart }()
	privateEnsureReleaseBinary = func(context.Context, install.ReleaseBinaryOptions) (string, error) { return target, nil }
	calls := 0
	privateStartUpgradeHelper = func(_ context.Context, options install.UpgradeHelperLaunchOptions) (install.UpgradeHelperLaunchResult, error) {
		calls++
		if options.State.PendingUpgrade == nil || options.State.PendingUpgrade.Phase != install.PendingUpgradePhasePrepared || options.State.PendingUpgrade.TargetBinaryPath != target {
			t.Fatal("helper ran before prepared journal")
		}
		return install.UpgradeHelperLaunchResult{UnitName: "fixture"}, nil
	}
	now := time.Now().UTC()
	stateValue.PendingUpgrade = &install.PendingUpgrade{Phase: install.PendingUpgradePhaseAvailable, Source: install.UpgradeSourceRelease, TargetVersion: "v2.0.0", TargetSlot: "v2.0.0", RequestedAt: &now}
	attempted, err := prepareAndLaunchPrivateUpgrade(t.Context(), host, &stateValue, &orchestrator.PrivateUpgradeFlow{TargetVersion: "v2.0.0", Source: string(install.UpgradeSourceRelease)}, privatechat.Owner{})
	if err != nil || !attempted || calls != 1 {
		t.Fatalf("launch: attempted=%v calls=%d err=%v", attempted, calls, err)
	}
	saved, err := install.LoadState(path)
	if err != nil || saved.PendingUpgrade == nil || saved.PendingUpgrade.Phase != install.PendingUpgradePhasePrepared || saved.PendingUpgrade.HelperUnitName != "fixture" {
		t.Fatalf("install journal: %#v %v", saved.PendingUpgrade, err)
	}
}

func TestPrivateUpgradeCancellationBeforeExecutionNeverLaunches(t *testing.T) {
	host, _ := newUpgradeTestApp(t, &lifecycleGateway{})
	host.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v2.0.0"}, nil
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	apply := func(id string, action control.Action) privatechat.EffectRecord {
		t.Helper()
		raw, _ := json.Marshal(action)
		seq, _, e := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		effects, e := p.ApplyIngress(t.Context(), o, 1, seq)
		if e != nil || len(effects) != 1 {
			t.Fatalf("apply %s: %#v %v", id, effects, e)
		}
		return effects[0]
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "card"}
	check := base
	check.Kind = control.ActionUpgradeCommand
	check.Text = "/upgrade latest"
	checkEffect := apply("check", check)
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, checkEffect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	confirm := base
	confirm.Kind = control.ActionUpgradeOwnerFlow
	confirm.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionConfirm}
	confirmEffect := apply("confirm", confirm)
	running, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, confirmEffect.Effect.ID)
	if err != nil || len(running) != 2 {
		t.Fatalf("confirm: %v", err)
	}
	gateway := &privatePublishGateway{}
	host.gateway = gateway
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	cancelAction := base
	cancelAction.Kind = control.ActionUpgradeOwnerFlow
	cancelAction.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionCancel}
	cancelEffect := apply("cancel", cancelAction)
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, cancelEffect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	original := privateEnsureReleaseBinary
	defer func() { privateEnsureReleaseBinary = original }()
	calls := 0
	privateEnsureReleaseBinary = func(context.Context, install.ReleaseBinaryOptions) (string, error) { calls++; return "", nil }
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, running[1].Effect.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "cancelled" {
		t.Fatalf("cancel did not prevent launch: %d", calls)
	}
}

func TestPrivateUpgradeCancellationDuringDownloadStopsOnce(t *testing.T) {
	host, _ := newUpgradeTestApp(t, &lifecycleGateway{})
	host.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v2.0.0"}, nil
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	apply := func(id string, action control.Action) privatechat.EffectRecord {
		t.Helper()
		raw, _ := json.Marshal(action)
		seq, _, e := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		effects, e := p.ApplyIngress(t.Context(), o, 1, seq)
		if e != nil || len(effects) != 1 {
			t.Fatalf("apply %s: %#v %v", id, effects, e)
		}
		return effects[0]
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "card"}
	check := base
	check.Kind = control.ActionUpgradeCommand
	check.Text = "/upgrade latest"
	checkEffect := apply("check", check)
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, checkEffect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	confirm := base
	confirm.Kind = control.ActionUpgradeOwnerFlow
	confirm.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionConfirm}
	confirmEffect := apply("confirm", confirm)
	running, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, confirmEffect.Effect.ID)
	if err != nil || len(running) != 2 {
		t.Fatalf("confirm: %v", err)
	}
	gateway := &privatePublishGateway{}
	host.gateway = gateway
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	original := privateEnsureReleaseBinary
	defer func() { privateEnsureReleaseBinary = original }()
	started := make(chan struct{})
	calls := 0
	privateEnsureReleaseBinary = func(ctx context.Context, _ install.ReleaseBinaryOptions) (string, error) {
		calls++
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, e := runPrivateUpgradeCommand(context.Background(), host, p, j, o, 1, running[1].Effect.ID)
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	cancelAction := base
	cancelAction.Kind = control.ActionUpgradeOwnerFlow
	cancelAction.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionCancel}
	cancelEffect := apply("cancel", cancelAction)
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, cancelEffect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-done:
		if e != nil && !errors.Is(e, privatechat.ErrDeferred) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("download did not stop")
	}
	if app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "cancelled" {
		if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, running[1].Effect.ID); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "cancelled" {
		t.Fatalf("cancel failed: calls=%d flow=%#v", calls, app.Service().PrivateUpgradeFlow(o.SurfaceID))
	}
}

func TestPrivateUpgradeUnknownHelperReconcilesOnlyTerminalInstallEvidence(t *testing.T) {
	host, path := newUpgradeTestApp(t, &lifecycleGateway{})
	host.upgradeRuntime.Lookup = func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error) {
		return install.ReleaseInfo{TagName: "v2.0.0"}, nil
	}
	current, err := install.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(current.CurrentBinaryPath, []byte("current"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err = os.WriteFile(target, []byte("target"), 0700); err != nil {
		t.Fatal(err)
	}
	originalEnsure, originalStart := privateEnsureReleaseBinary, privateStartUpgradeHelper
	defer func() { privateEnsureReleaseBinary = originalEnsure; privateStartUpgradeHelper = originalStart }()
	privateEnsureReleaseBinary = func(context.Context, install.ReleaseBinaryOptions) (string, error) { return target, nil }
	starts := 0
	privateStartUpgradeHelper = func(_ context.Context, options install.UpgradeHelperLaunchOptions) (install.UpgradeHelperLaunchResult, error) {
		starts++
		state := options.State
		state.PendingUpgrade.Phase = install.PendingUpgradePhaseCommitted
		if e := install.WriteState(options.StatePath, state); e != nil {
			t.Fatal(e)
		}
		return install.UpgradeHelperLaunchResult{}, fmt.Errorf("helper response lost")
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	dbPath := filepath.Join(t.TempDir(), "private.db")
	j, err := privatechat.OpenJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	apply := func(id string, action control.Action) privatechat.EffectRecord {
		t.Helper()
		raw, _ := json.Marshal(action)
		seq, _, e := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		out, e := p.ApplyIngress(t.Context(), o, 1, seq)
		if e != nil || len(out) != 1 {
			t.Fatalf("apply: %#v %v", out, e)
		}
		return out[0]
	}
	base := control.Action{GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "card"}
	check := base
	check.Kind = control.ActionUpgradeCommand
	check.Text = "/upgrade latest"
	effect := apply("check", check)
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effect.Effect.ID); err != nil {
		t.Fatal(err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	confirm := base
	confirm.Kind = control.ActionUpgradeOwnerFlow
	confirm.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flow.ID, OptionID: upgradeOwnerActionConfirm}
	effect = apply("confirm", confirm)
	running, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effect.Effect.ID)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &privatePublishGateway{}
	host.gateway = gateway
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	executeID := running[1].Effect.ID
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, executeID); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("lost helper result: %v", err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, executeID)
	if err != nil || len(result) != 1 {
		t.Fatalf("terminal reconciliation: %#v %v", result, err)
	}
	if starts != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "completed" {
		t.Fatalf("duplicate helper or missing terminal: starts=%d", starts)
	}
}

func TestPrivateLocalUpgradeFreezesArtifactAndOwnerBeforeInstaller(t *testing.T) {
	if !install.CurrentBuildAllowsLocalUpgrade() {
		t.Skip("build policy disables local upgrade")
	}
	host, path := newUpgradeTestApp(t, &lifecycleGateway{})
	stateValue, err := install.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	artifact := install.LocalUpgradeArtifactPath(stateValue)
	if err = os.MkdirAll(filepath.Dir(artifact), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(artifact, []byte("approved local binary"), 0700); err != nil {
		t.Fatal(err)
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	action := control.Action{Kind: control.ActionUpgradeCommand, Text: "/upgrade local", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "input"}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "local", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatalf("admit: %#v %v", effects, err)
	}
	running, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID)
	if err != nil || len(running) != 2 {
		t.Fatalf("prepare: %#v %v", running, err)
	}
	gateway := &privatePublishGateway{}
	host.gateway = gateway
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	if flow.MessageID == "" || flow.SourceSHA256 == "" {
		t.Fatalf("missing frozen receipt: %#v", flow)
	}
	original := privateRunLocalUpgrade
	defer func() { privateRunLocalUpgrade = original }()
	calls := 0
	privateRunLocalUpgrade = func(opts install.LocalBinaryUpgradeOptions) (string, error) {
		calls++
		if opts.GatewayID != o.GatewayID || opts.SurfaceSessionID != o.SurfaceID || opts.ChatID != o.ChatID || opts.ActorUserID != o.UserID || opts.PrivateRequestID != flow.ID || opts.SourceSHA256 != flow.SourceSHA256 || opts.SourceBinary == artifact {
			t.Fatalf("local intent: %#v", opts)
		}
		b, e := os.ReadFile(opts.SourceBinary)
		if e != nil || string(b) != "approved local binary" {
			t.Fatalf("frozen artifact: %q %v", b, e)
		}
		return "slot", nil
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, running[1].Effect.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("installer calls=%d", calls)
	}
}
