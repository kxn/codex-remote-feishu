package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type retryingPrivatePublicationFixture struct {
	release   <-chan struct{}
	currentID string
}

func (f *retryingPrivatePublicationFixture) PrepareOperation(_ context.Context, _ privatechat.Owner, _ int64, id string, _ int, raw json.RawMessage) (json.RawMessage, error) {
	if id == f.currentID {
		return raw, nil
	}
	<-f.release
	return nil, errors.New("publication preparation remains unavailable")
}

func (*retryingPrivatePublicationFixture) ConfirmOperation(context.Context, privatechat.Owner, int64, string, int) error {
	return nil
}

func (*retryingPrivatePublicationFixture) ValidateOperation(context.Context, privatechat.Owner, int64, string, int, json.RawMessage) error {
	return nil
}

func TestPrivateEffectRunnerAdmitsPublicationPastRetryingBacklog(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	journal, err := privatechat.OpenJournal(filepath.Join(root, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	carriers, err := openPrivateNativeHostStore(filepath.Join(root, "carriers.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer carriers.Close()
	gateway := &privatePublishGateway{}
	host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	seed := privatechat.New(nil, orchestrator.Config{})
	if err = seed.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := privatechat.EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: owner.SurfaceID, Notice: &control.Notice{Text: "reply"}})
	if err != nil {
		t.Fatal(err)
	}
	var effects []privatechat.Effect
	const historical = 140
	for i := range historical {
		effects = append(effects, privatechat.Effect{ID: fmt.Sprintf("historical-%d", i), Lane: "interactive", Body: body})
	}
	current := privatechat.Effect{ID: "current", Lane: "interactive", Body: body}
	effects = append(effects, current)
	if err = journal.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: owner, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	processor, err := privatechat.NewProcessor(app, journal, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	ownership := &retryingPrivatePublicationFixture{currentID: current.ID}
	runner := &privateWorkRunner{
		host:      host,
		journal:   journal,
		processor: processor,
		ingress:   &applicationIngress{},
		native:    &privateNativeHost{store: carriers},
		publisher: &privatePublisher{host: host, processor: processor, journal: journal, ownership: ownership},
	}
	for range historical/8 + 1 {
		release := make(chan struct{})
		ownership.release = release
		err = runner.pass(ctx)
		close(release)
		runner.effectWG.Wait()
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := journal.LoadEffect(ctx, owner, 1, current.ID)
	if err != nil || got.State != "confirmed" || gateway.calls != 1 {
		t.Fatalf("current publication starved by retrying history: state=%s calls=%d err=%v", got.State, gateway.calls, err)
	}
}

func TestPrivateEffectRunnerRotatesNormalAndControlWithoutOvertakingOwnerInput(t *testing.T) {
	ctx := t.Context()
	journal, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	foreign := privatechat.Owner{GatewayID: "g", SurfaceID: "foreign", ChatID: "other-chat", UserID: "other-user", TenantID: "tenant", AppID: "bot"}
	commit := func(o privatechat.Owner, revision int64, effects []privatechat.Effect) {
		t.Helper()
		seed := privatechat.New(nil, orchestrator.Config{})
		if e := seed.RegisterOwner(o); e != nil {
			t.Fatal(e)
		}
		core, e := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if e != nil {
			t.Fatal(e)
		}
		if e = journal.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: 1, Revision: revision, Core: core}, revision-1, effects); e != nil {
			t.Fatal(e)
		}
	}
	effect := func(id string, event eventcontract.Event) privatechat.Effect {
		t.Helper()
		body, e := privatechat.EncodeEffectEvent(event)
		if e != nil {
			t.Fatal(e)
		}
		return privatechat.Effect{ID: id, Lane: "background", Body: body}
	}
	input := func(id string, o privatechat.Owner, kind agentproto.CommandKind) privatechat.Effect {
		item := effect(id, eventcontract.Event{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: o.SurfaceID, Command: &agentproto.Command{Kind: kind}})
		var wire map[string]json.RawMessage
		if e := json.Unmarshal(item.Body, &wire); e != nil {
			t.Fatal(e)
		}
		wire["target_instance_id"] = json.RawMessage(`"carrier"`)
		body, e := json.Marshal(wire)
		if e != nil {
			t.Fatal(e)
		}
		item.Body = body
		return item
	}
	daemonEffect := func(id string, o privatechat.Owner, kind control.DaemonCommandKind) privatechat.Effect {
		return effect(id, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: o.SurfaceID, DaemonCommand: &control.DaemonCommand{Kind: kind}})
	}
	older := input("older-input", owner, agentproto.CommandPromptSend)
	younger := input("younger-input", owner, agentproto.CommandTurnSteer)
	effects := []privatechat.Effect{older}
	for i := range 9 {
		effects = append(effects, daemonEffect(fmt.Sprintf("normal-%d", i), owner, control.DaemonCommandDebug))
	}
	for i := range 5 {
		effects = append(effects, daemonEffect(fmt.Sprintf("control-%d", i), owner, control.DaemonCommandKillHeadless))
	}
	commit(owner, 1, append(effects, younger))
	currentNormal := input("current-normal", foreign, agentproto.CommandPromptSend)
	currentControl := daemonEffect("current-control", foreign, control.DaemonCommandKillHeadless)
	commit(foreign, 1, nil)
	commit(foreign, 2, []privatechat.Effect{currentNormal, currentControl})
	runner := &privateWorkRunner{journal: journal}
	admissions := make(map[string]int)
	for round := range 2 {
		release := make(chan struct{})
		err = runner.scheduleHostEffectRounds(ctx, ctx, func(work privatechat.PendingEffectWork, route privateEffectRoute) privateEffectAdmission {
			id := work.Effect.Effect.ID
			result := runner.startEffect(id, route.inputKey, route.pool, func() { <-release })
			if result == privateEffectAdmitted {
				admissions[id]++
			}
			return result
		})
		close(release)
		runner.effectWG.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if round == 0 && (admissions[currentNormal.ID] != 0 || admissions[currentControl.ID] != 0) {
			t.Fatalf("retry backlog did not fill both pools: %v", admissions)
		}
	}
	if admissions[currentNormal.ID] != 1 || admissions[currentControl.ID] != 1 ||
		admissions[older.ID] != 2 || admissions[younger.ID] != 0 {
		t.Fatalf("rotation starved a pool or overtook the owner's earlier input: %v", admissions)
	}
}

func TestPrivateEffectRunnerIsolatesBlockedWorkAndDeduplicates(t *testing.T) {
	r := &privateWorkRunner{}
	blocked := make(chan struct{})
	started := make(chan struct{}, 16)
	for i := 0; i < 8; i++ {
		r.startEffect(string(rune('a'+i)), "", privateEffectNormal, func() { started <- struct{}{}; <-blocked })
	}
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("normal work failed to start")
		}
	}
	cancel := make(chan struct{}, 1)
	r.startEffect("cancel", "", privateEffectControl, func() { cancel <- struct{}{} })
	select {
	case <-cancel:
	case <-time.After(time.Second):
		t.Fatal("cancel blocked behind slow work")
	}
	publication := make(chan struct{}, 1)
	r.startEffect("menu", "", privateEffectPublication, func() { publication <- struct{}{} })
	select {
	case <-publication:
	case <-time.After(time.Second):
		t.Fatal("publication blocked behind slow work")
	}
	duplicate := make(chan struct{}, 1)
	r.startEffect("a", "", privateEffectControl, func() { duplicate <- struct{}{} })
	select {
	case <-duplicate:
		t.Fatal("same effect ran twice")
	default:
	}
	close(blocked)
	r.effectWG.Wait()
}

func TestPrivateEffectRunnerSerializesOwnerInput(t *testing.T) {
	r := &privateWorkRunner{}
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	next := make(chan struct{}, 1)
	r.startEffect("first", "owner-input", privateEffectNormal, func() { started <- struct{}{}; <-release })
	<-started
	r.startEffect("second", "owner-input", privateEffectNormal, func() { next <- struct{}{} })
	select {
	case <-next:
		t.Fatal("owner input ran concurrently")
	default:
	}
	close(release)
	r.effectWG.Wait()
	r.startEffect("second", "owner-input", privateEffectNormal, func() { next <- struct{}{} })
	select {
	case <-next:
	case <-time.After(time.Second):
		t.Fatal("next input did not start")
	}
	r.effectWG.Wait()
}
