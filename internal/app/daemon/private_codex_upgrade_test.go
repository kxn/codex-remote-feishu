package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateCodexUpgradeOriginalCardAndSharedRequestRecovery(t *testing.T) {
	host, _ := newUpgradeTestApp(t, &lifecycleGateway{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	journalPath := filepath.Join(t.TempDir(), "private.db")
	j, err := privatechat.OpenJournal(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
	store, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	installation := stubStandaloneCodexInstallation("0.1.0")
	coordinator, err := workupgrade.New(store, workupgrade.Config{HostID: "host", StateDir: t.TempDir(), ConfiguredBinary: "codex", Inspect: func(context.Context, codexupgrade.InspectOptions) codexupgrade.Installation { return installation }, Drain: func(context.Context, work.WorkerSession) error { t.Fatal("UI started native drain"); return nil }, Restart: func(context.Context, string, string) error { t.Fatal("UI restarted native carrier"); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	host.teamRuntime = &teamRuntime{store: store, codexUpgrader: coordinator}
	checks := 0
	host.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return installation, nil
	}
	host.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) { checks++; return "0.2.0", nil }
	flowID := ""
	cardID := "message"
	host.gateway = &privatePublishGateway{}
	publish := func(effect privatechat.EffectRecord) {
		t.Helper()
		pub := &privatePublisher{host: host, processor: p, journal: j}
		if _, err := pub.Publish(t.Context(), o, 1, effect.Effect.ID); err != nil {
			t.Fatal(err)
		}
	}
	admit := func(id, text, option string) privatechat.EffectRecord {
		t.Helper()
		action := control.Action{Kind: control.ActionUpgradeCommand, Text: text, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: cardID}
		if option != "" {
			action.Kind = control.ActionUpgradeOwnerFlow
			if flow := app.Service().PrivateUpgradeFlow(o.SurfaceID); flow != nil {
				flowID = flow.ID
			}
			action.OwnerFlow = &control.ActionOwnerCardFlow{FlowID: flowID, OptionID: option}
		}
		raw, _ := json.Marshal(action)
		seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if err != nil {
			t.Fatal(err)
		}
		effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
		if err != nil || len(effects) != 1 {
			t.Fatalf("admit %s: %v %#v", id, err, effects)
		}
		return effects[0]
	}
	run := func(effect privatechat.EffectRecord) []privatechat.EffectRecord {
		t.Helper()
		out, err := runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, effect.Effect.ID)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	opened := run(admit("open", "/upgrade codex", ""))
	if checks != 0 || len(opened) != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "open" {
		t.Fatal("opening changed legacy explicit-check behavior")
	}
	publish(opened[0])
	cardID = "platform-1"
	for i, id := range []string{"check", "recheck"} {
		out := run(admit(id, "", upgradeOwnerActionCheck))
		if checks != i+1 || len(out) != 1 {
			t.Fatal("check is not repeatable")
		}
		event, _, err := privatechat.DecodeStoredEffect(out[0].Effect.Body)
		if err != nil || event.SourceMessageID != cardID || !event.InlineReplaceCurrentCard {
			t.Fatalf("check appended a card: %#v %v", event, err)
		}
		publish(out[0])
	}
	// Reopen the journal as the daemon would after a process restart.
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(journalPath)
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
	running := run(admit("confirm", "", upgradeOwnerActionConfirm))
	if len(running) != 2 {
		t.Fatalf("missing durable execute: %#v", running)
	}
	// Preserve an unknown display operation while recovering the separate
	// confirmed business intent. Display delivery is not the authorization.
	host.gateway = &privatePublishGateway{fail: true}
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	if _, err = publisher.Publish(t.Context(), o, 1, running[0].Effect.ID); !errors.Is(err, errPrivatePublicationUnknown) {
		t.Fatalf("progress uncertainty: %v", err)
	}
	flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
	req := workupgrade.PrivateRequest{ID: "private-codex:" + running[1].Effect.ID, TargetVersion: flow.TargetVersion, InstallationFingerprint: flow.StateFingerprint, Source: workstore.NativeUpgradePrivateSource{Tenant: o.TenantID, AppID: o.AppID, Gateway: o.GatewayID, Surface: o.SurfaceID, Chat: o.ChatID, User: o.UserID, Generation: 1, EffectID: running[1].Effect.ID}}
	if err = j.MarkEffectSending(t.Context(), o, 1, running[1].Effect.ID); err != nil {
		t.Fatal(err)
	}
	registered, err := coordinator.RequestPrivate(t.Context(), req, privateCodexUpgradeAuthority{p})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.ResolveEffect(t.Context(), o, 1, running[1].Effect.ID, "unknown", []byte(`{"crash":"after shared registration"}`), nil); err != nil {
		t.Fatal(err)
	}
	resumed := run(running[1])
	if len(resumed) != 2 || app.Service().PrivateUpgradeFlow(o.SurfaceID).SharedRequestID != registered.ID {
		t.Fatal("unknown intent did not recover its exact shared request")
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, resumed[1].Effect.ID); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("unfinished maintenance claimed complete: %v", err)
	}
	if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		current, err := tx.GetNativeUpgrade(req.ID)
		if err != nil {
			return err
		}
		revision := current.Revision
		current.Revision++
		current.Stage = "unknown"
		current.LastError = "安装进程退出回执尚未核实"
		return tx.PutNativeUpgrade(current, revision)
	}); err != nil {
		t.Fatal(err)
	}
	progress := run(resumed[1])
	if len(progress) != 2 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "restarting" {
		t.Fatal("uncertain maintenance lost its progress receipt")
	}
	if _, err = runPrivateUpgradeCommand(t.Context(), host, p, j, o, 1, progress[1].Effect.ID); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("unchanged maintenance polled into another card: %v", err)
	}
	// A recorded coordinator completion is the UI's sole completion source.
	// Installation and native resumption themselves are tested by the shared
	// maintenance integration fixture, not simulated by this card test.
	if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		current, err := tx.GetNativeUpgrade(req.ID)
		if err != nil {
			return err
		}
		revision := current.Revision
		current.Revision++
		current.Stage = "completed"
		return tx.PutNativeUpgrade(current, revision)
	}); err != nil {
		t.Fatal(err)
	}
	completed := run(progress[1])
	if len(completed) != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "completed" {
		t.Fatal("shared completion did not finish original private owner")
	}
	event, _, err := privatechat.DecodeStoredEffect(completed[0].Effect.Body)
	if err != nil || !event.InlineReplaceCurrentCard || event.SourceMessageID != cardID {
		t.Fatal("completion did not replace original card")
	}
	if replay := run(running[1]); len(replay) != len(resumed) {
		t.Fatal("execution replay lost committed effects")
	}
	for _, id := range []string{"repeat-confirm", "repeat-cancel"} {
		option := upgradeOwnerActionConfirm
		if id == "repeat-cancel" {
			option = upgradeOwnerActionCancel
		}
		rejected := run(admit(id, "", option))
		if len(rejected) != 1 || app.Service().PrivateUpgradeFlow(o.SurfaceID).Phase != "completed" {
			t.Fatal("stale operation changed completed maintenance")
		}
		event, _, err := privatechat.DecodeStoredEffect(rejected[0].Effect.Body)
		if err != nil || event.Notice == nil || event.Notice.Code != "codex_upgrade_owner_expired" {
			t.Fatalf("stale operation was not explained: %v %#v", err, event)
		}
	}
	if checks != 2 {
		t.Fatal("confirmation/recovery unexpectedly looked up another version")
	}
}
