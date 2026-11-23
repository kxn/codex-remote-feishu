package ingressowner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type resolverFunc func(context.Context, Event, *PlatformMessage) (Owner, error)

func (f resolverFunc) Resolve(ctx context.Context, e Event, m *PlatformMessage) (Owner, error) {
	return f(ctx, e, m)
}

type verifier struct{}

func (verifier) VerifyPlatformReceipt(_ context.Context, m PlatformMessage, proof []byte) error {
	if string(proof) != "platform:"+m.MessageID {
		return errors.New("unverified receipt")
	}
	return nil
}
func (verifier) VerifyPrivateBinding(_ context.Context, b PrivateBinding, proof []byte) error {
	if string(proof) != "binding:"+b.ChatID {
		return errors.New("unverified binding")
	}
	return nil
}

type admitFunc func(context.Context, Record) (json.RawMessage, error)

func (f admitFunc) Admit(ctx context.Context, r Record) (AdmissionReceipt, error) {
	response, err := f(ctx, r)
	if err != nil {
		return AdmissionReceipt{}, err
	}
	hash, err := AdmissionHash(r)
	if err != nil {
		return AdmissionReceipt{}, err
	}
	return AdmissionReceipt{Key: r.Event.Key, Owner: *r.Owner, AdmissionID: "stored:" + r.Event.Key.EventID, PayloadHash: hash, Response: response}, nil
}
func openTest(t *testing.T, path string) *Router {
	t.Helper()
	r, e := Open(path, verifier{}, verifier{})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func testEvent(id string, kind Kind) Event {
	return Event{Key: Key{Tenant: "tenant", AppID: "app", EventID: id}, Source: Source{GatewayID: "gateway", BindingGeneration: 1, ReceivedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}, Kind: kind, Payload: json.RawMessage(`{"sdk":"fact"}`), Operator: Operator{IDType: "open_id", ID: "user"}}
}

var privateOwner = Owner{Application: Private, ID: "alice", Epoch: 1}
var teamOwner = Owner{Application: Team, ID: "project", Epoch: 1}

func classify(_ context.Context, e Event, m *PlatformMessage) (Owner, error) {
	if m != nil {
		return m.Owner, nil
	}
	if e.Kind == Menu || e.ChatType == "p2p" {
		return privateOwner, nil
	}
	return teamOwner, nil
}
func TestDurableRoutingCrossApplicationAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	defer r.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode: %v %v", info, err)
	}
	if second, e := Open(path, verifier{}, verifier{}); e == nil {
		second.Close()
		t.Fatal("second writer opened")
	}
	p := testEvent("private-event", Message)
	p.ChatType = "p2p"
	p.ChatID = "private-chat"
	p.MessageID = "private-message"
	v, replay, err := r.Admit(t.Context(), p, resolverFunc(classify))
	if err != nil || replay || v.Owner == nil || *v.Owner != privateOwner {
		t.Fatalf("private: %+v %v", v, err)
	}
	g := testEvent("group-event", Message)
	g.ChatType = "group"
	g.ChatID = "group-chat"
	g.MessageID = "group-message"
	v, _, err = r.Admit(t.Context(), g, resolverFunc(classify))
	if err != nil || v.Owner == nil || *v.Owner != teamOwner {
		t.Fatalf("group: %+v %v", v, err)
	}
	p.Source.BindingGeneration = 2
	p.Source.GatewayID = "rebound-gateway"
	v, replay, err = r.Admit(t.Context(), p, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) {
		t.Fatal("duplicate re-resolved")
		return teamOwner, nil
	}))
	if err != nil || !replay || *v.Owner != privateOwner || v.Event.Source.BindingGeneration != 1 || v.Event.Source.GatewayID != "gateway" {
		t.Fatalf("replay lost owner/source: %+v %v", v, err)
	}
	p.Payload = json.RawMessage(`{"sdk":"changed"}`)
	if _, _, err = r.Admit(t.Context(), p, resolverFunc(classify)); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed facts accepted: %v", err)
	}
}
func TestBindingsAndUnknownMessageRouting(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	ctx := t.Context()
	m := PlatformMessage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, ChatID: "private-chat", MessageID: "card", Owner: privateOwner, Version: 1, Lifecycle: "active", CardRef: "view-1", CardKind: "private_view"}
	if err := r.RecordPublished(ctx, m, []byte("forged"), 0); err == nil {
		t.Fatal("forged receipt accepted")
	}
	reaction := testEvent("reaction", Reaction)
	reaction.MessageID = "card"
	v, _, err := r.Admit(ctx, reaction, resolverFunc(classify))
	if err != nil || v.State != Unresolved {
		t.Fatalf("missing message guessed: %+v %v", v, err)
	}
	if err = r.RecordPublished(ctx, m, []byte("platform:card"), 0); err != nil {
		t.Fatal(err)
	}
	v, err = r.Resolve(ctx, reaction.Key, resolverFunc(classify))
	if err != nil || v.State != Pending || *v.Owner != privateOwner {
		t.Fatalf("reaction failed: %+v %v", v, err)
	}
	card := testEvent("card-event", Card)
	card.ChatID = "private-chat"
	card.MessageID = "card"
	card.CardRef = "view-1"
	card.CardKind = "private_view"
	v, _, err = r.Admit(ctx, card, resolverFunc(classify))
	if err != nil || v.Owner == nil || *v.Owner != privateOwner || v.Route == nil || v.Route.Message == nil || v.Route.Message.Version != 1 {
		t.Fatalf("card: %+v %v", v, err)
	}
	updated := m
	updated.Version = 2
	updated.Lifecycle = "replaced"
	if err = r.RecordPublished(ctx, updated, []byte("platform:card"), 1); err != nil {
		t.Fatal(err)
	}
	reused := updated
	reused.Version = 3
	reused.Lifecycle = "active"
	if err = r.RecordPublished(ctx, reused, []byte("platform:card"), 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused old card token accepted: %v", err)
	}
	frozen, err := r.Get(ctx, card.Key)
	if err != nil || frozen.Route == nil || frozen.Route.Message == nil || frozen.Route.Message.Version != 1 || frozen.Route.Message.Lifecycle != "active" {
		t.Fatalf("original card route changed: %+v %v", frozen, err)
	}
	other := m
	other.ChatID = "group-chat"
	other.Owner = teamOwner
	other.CardKind = "team_view"
	if err = r.RecordPublished(ctx, other, []byte("platform:card"), 0); err != nil {
		t.Fatal(err)
	}
	ambiguous := testEvent("ambiguous", Recall)
	ambiguous.MessageID = "card"
	v, _, err = r.Admit(ctx, ambiguous, resolverFunc(classify))
	if err != nil || v.State != Unresolved {
		t.Fatalf("ambiguous recall guessed owner: %+v %v", v, err)
	}
	menu := testEvent("menu", Menu)
	v, _, err = r.Admit(ctx, menu, resolverFunc(classify))
	if err != nil || v.State != Unresolved {
		t.Fatalf("menu guessed chat: %+v %v", v, err)
	}
	binding := PrivateBinding{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, Operator: menu.Operator, ChatID: "private-chat", Owner: privateOwner}
	if err = r.RecordPrivateBinding(ctx, binding, []byte("forged")); err == nil {
		t.Fatal("forged binding accepted")
	}
	if err = r.RecordPrivateBinding(ctx, binding, []byte("binding:private-chat")); err != nil {
		t.Fatal(err)
	}
	v, err = r.Resolve(ctx, menu.Key, resolverFunc(classify))
	if err != nil || v.Owner == nil || *v.Owner != privateOwner || v.Route == nil || v.Route.Private == nil || v.Route.Private.ChatID != "private-chat" {
		t.Fatalf("menu: %+v %v", v, err)
	}
}
func TestDeliveryReplayAndConcurrentSingleDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	e := testEvent("event", Message)
	e.ChatType = "p2p"
	e.ChatID = "chat"
	e.MessageID = "message"
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	// Application persists once and the process crashes before router acceptance.
	applicationCommitted := map[Key]int{}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected simulated crash")
			}
		}()
		_, _ = r.Deliver(t.Context(), e.Key, admitFunc(func(_ context.Context, record Record) (json.RawMessage, error) {
			applicationCommitted[record.Event.Key]++
			panic("crash after application commit")
		}))
	}()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	var mu sync.Mutex
	calls := 0
	app := admitFunc(func(_ context.Context, record Record) (json.RawMessage, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if record.Owner == nil || *record.Owner != privateOwner || record.Event.Source.BindingGeneration != 1 {
			t.Error("lost frozen identity")
		}
		if applicationCommitted[record.Event.Key] == 0 {
			applicationCommitted[record.Event.Key]++
		}
		time.Sleep(10 * time.Millisecond)
		return json.RawMessage(`{"toast":"saved"}`), nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			record, err := r.Deliver(context.Background(), e.Key, app)
			if err == nil && (record.State != Accepted || string(record.Response) != `{"toast":"saved"}`) {
				err = fmt.Errorf("bad response: %+v", record)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 || applicationCommitted[e.Key] != 1 {
		t.Fatalf("duplicate effects: calls=%d committed=%d", calls, applicationCommitted[e.Key])
	}
	v, err := r.Deliver(t.Context(), e.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
		t.Fatal("accepted event redelivered")
		return nil, nil
	}))
	if err != nil || string(v.Response) != `{"toast":"saved"}` {
		t.Fatalf("response replay: %+v %v", v, err)
	}
}

func TestDifferentOwnersDoNotShareDeliveryLock(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	first := testEvent("private", Message)
	first.ChatType = "p2p"
	first.ChatID = "private"
	first.MessageID = "private-message"
	second := testEvent("group", Message)
	second.ChatType = "group"
	second.ChatID = "group"
	second.MessageID = "group-message"
	for _, e := range []Event{first, second} {
		if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
			t.Fatal(err)
		}
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := r.Deliver(context.Background(), first.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
			close(entered)
			<-release
			return json.RawMessage(`{}`), nil
		}))
		done <- err
	}()
	<-entered
	other := make(chan error, 1)
	go func() {
		_, err := r.Deliver(context.Background(), second.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }))
		other <- err
	}()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("one app blocked another event")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestOutstandingAndReceiptVersionRace(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	ctx := t.Context()
	m := PlatformMessage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, ChatID: "chat", MessageID: "card", Owner: privateOwner, Version: 1, Lifecycle: "active", CardRef: "view-1", CardKind: "private_view"}
	if err := r.RecordPublished(ctx, m, []byte("platform:card"), 0); err != nil {
		t.Fatal(err)
	}
	e := testEvent("callback", Card)
	e.ChatID = "chat"
	e.MessageID = "card"
	e.CardRef = "view-1"
	e.CardKind = "private_view"
	changed := resolverFunc(func(ctx context.Context, _ Event, _ *PlatformMessage) (Owner, error) {
		next := m
		next.Version = 2
		next.Lifecycle = "replaced"
		if err := r.RecordPublished(ctx, next, []byte("platform:card"), 1); err != nil {
			return Owner{}, err
		}
		return privateOwner, nil
	})
	if _, _, err := r.Admit(ctx, e, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale card state accepted: %v", err)
	}
	pending, err := r.Outstanding(ctx, 0, 10)
	if err != nil || len(pending) != 1 || pending[0].State != Unresolved {
		t.Fatalf("lost unresolved: %+v %v", pending, err)
	}
	v, err := r.Resolve(ctx, e.Key, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) {
		t.Fatal("expired card reached resolver")
		return Owner{}, nil
	}))
	if err != nil || v.State != Expired {
		t.Fatalf("stale card not expired: %+v %v", v, err)
	}
}
func TestApplicationFailureRemainsRetryable(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	e := testEvent("event", Message)
	e.ChatType = "p2p"
	e.ChatID = "chat"
	e.MessageID = "message"
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Deliver(t.Context(), e.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { return nil, errors.New("temporary failure") })); err == nil {
		t.Fatal("failure hidden")
	}
	pending, err := r.Outstanding(t.Context(), 0, 10)
	if err != nil || len(pending) != 1 || pending[0].State != Pending {
		t.Fatalf("lost pending: %+v %v", pending, err)
	}
	v, err := r.Deliver(t.Context(), e.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { return json.RawMessage(`{"ok":true}`), nil }))
	if err != nil || v.State != Accepted {
		t.Fatalf("retry failed: %+v %v", v, err)
	}
}

type rawAdmitter func(context.Context, Record) (AdmissionReceipt, error)

func (a rawAdmitter) Admit(ctx context.Context, r Record) (AdmissionReceipt, error) { return a(ctx, r) }
func TestTypedAdmissionReceiptMustMatchFrozenEvent(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	e := testEvent("typed", Message)
	e.ChatType = "p2p"
	e.ChatID = "chat"
	e.MessageID = "message"
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range []AdmissionReceipt{{}, {Key: e.Key, Owner: teamOwner, AdmissionID: "id", PayloadHash: "bad"}, {Key: e.Key, Owner: privateOwner, AdmissionID: "id", PayloadHash: "bad"}} {
		if _, err := r.Deliver(t.Context(), e.Key, rawAdmitter(func(context.Context, Record) (AdmissionReceipt, error) { return receipt, nil })); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid receipt accepted: %v", err)
		}
		saved, err := r.Get(t.Context(), e.Key)
		if err != nil || saved.State != Pending {
			t.Fatalf("invalid receipt lost input: %+v %v", saved, err)
		}
	}
	saved, err := r.Deliver(t.Context(), e.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
		return json.RawMessage(`{"toast":"stored"}`), nil
	}))
	if err != nil || saved.Receipt == nil || saved.Receipt.AdmissionID != "stored:typed" {
		t.Fatalf("valid receipt not saved: %+v %v", saved, err)
	}
}
func TestCardRequiresExactConfirmedVersionAndGeneration(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	ctx := t.Context()
	m := PlatformMessage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, ChatID: "chat", MessageID: "card", Owner: privateOwner, Version: 1, Lifecycle: "active", CardKind: "private_view", CardRef: "old"}
	if err := r.RecordPublished(ctx, m, []byte("platform:card"), 0); err != nil {
		t.Fatal(err)
	}
	old := testEvent("old-click", Card)
	old.ChatID = "chat"
	old.MessageID = "card"
	old.CardKind = "private_view"
	old.CardRef = "old"
	// A known click without a target remains unresolved, then must not be
	// interpreted against a later version that replaces its actual view token.
	pending := testEvent("pending-click", Card)
	pending.ChatID = "future-chat"
	pending.MessageID = "future-card"
	pending.CardKind = "private_view"
	pending.CardRef = "old"
	v, _, err := r.Admit(ctx, pending, resolverFunc(classify))
	if err != nil || v.State != Unresolved {
		t.Fatalf("pending: %+v %v", v, err)
	}
	next := m
	next.ChatID = "future-chat"
	next.MessageID = "future-card"
	next.Version = 2
	next.CardRef = "new"
	if err = r.RecordPublished(ctx, next, []byte("platform:future-card"), 0); err != nil {
		t.Fatal(err)
	}
	v, err = r.Resolve(ctx, pending.Key, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) {
		t.Fatal("old version dispatched")
		return Owner{}, nil
	}))
	if err != nil || v.State != Expired {
		t.Fatalf("old click not expired: %+v %v", v, err)
	}
	reaction := testEvent("reaction-after-rebind", Reaction)
	reaction.MessageID = "card"
	reaction.Source.BindingGeneration = 2
	observed, _, err := r.Admit(ctx, reaction, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) {
		t.Fatal("cross-generation reaction resolved")
		return Owner{}, nil
	}))
	if err != nil || observed.State != Unresolved {
		t.Fatalf("cross-generation reaction: %+v %v", observed, err)
	}
	fresh := old
	fresh.Key.EventID = "fresh-after-rebind"
	fresh.Source.BindingGeneration = 2
	v, _, err = r.Admit(ctx, fresh, resolverFunc(classify))
	if err != nil || v.State != Expired {
		t.Fatalf("generation mismatch: %+v %v", v, err)
	}
	missing := old
	missing.Key.EventID = "missing-version"
	missing.CardRef = ""
	missing.CardKind = ""
	v, _, err = r.Admit(ctx, missing, resolverFunc(classify))
	if err != nil || v.State != Expired {
		t.Fatalf("legacy card guessed: %+v %v", v, err)
	}
	first, _, err := r.Admit(ctx, old, resolverFunc(classify))
	if err != nil || first.State != Pending {
		t.Fatalf("first click: %+v %v", first, err)
	}
	old.Source.BindingGeneration = 2
	replay, dup, err := r.Admit(ctx, old, resolverFunc(classify))
	if err != nil || !dup || replay.State != Pending || replay.Event.Source.BindingGeneration != 1 {
		t.Fatalf("duplicate lost frozen generation: %+v %v", replay, err)
	}
}

func TestLeaderSelectionWaitsForVerifiedPublication(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	ctx := t.Context()
	click := testEvent("selection-click", Card)
	click.ChatID = "group-chat"
	click.MessageID = "selection-card"
	click.CardKind = "team_setup_ingress"
	click.CardRef = "source-ingress"
	first, _, err := r.Admit(ctx, click, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) {
		t.Fatal("unconfirmed card resolved")
		return Owner{}, nil
	}))
	if err != nil || first.State != Unresolved {
		t.Fatalf("unconfirmed selection: %+v %v", first, err)
	}
	// The adapter's trusted verifier must perform the existing notice/card/sender
	// checks; the callback token alone is not evidence of platform publication.
	message := PlatformMessage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, ChatID: "group-chat", MessageID: "selection-card", Owner: teamOwner, Version: 1, Lifecycle: "active", CardKind: "team_setup_ingress", CardRef: "source-ingress"}
	if err = r.RecordPublished(ctx, message, []byte("callback-only"), 0); err == nil {
		t.Fatal("callback alone proved send")
	}
	if err = r.RecordPublished(ctx, message, []byte("platform:selection-card"), 0); err != nil {
		t.Fatal(err)
	}
	resolved, err := r.Resolve(ctx, click.Key, resolverFunc(classify))
	if err != nil || resolved.State != Pending || resolved.Owner == nil || *resolved.Owner != teamOwner || resolved.Route == nil || resolved.Route.Message.CardRef != "source-ingress" {
		t.Fatalf("selection not resolved to original: %+v %v", resolved, err)
	}
}
