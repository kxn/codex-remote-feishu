package ingressowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type replacementAdmitter struct {
	token  string
	target string
	owner  *Owner
	calls  *int
}

func (a replacementAdmitter) Admit(_ context.Context, r Record) (AdmissionReceipt, error) {
	if a.calls != nil {
		*a.calls++
	}
	body := json.RawMessage(`{"card":{"title":"next"}}`)
	hash := sha256.Sum256(body)
	input, err := AdmissionHash(r)
	if err != nil {
		return AdmissionReceipt{}, err
	}
	owner := *r.Owner
	if a.owner != nil {
		owner = *a.owner
	}
	target := r.Event.MessageID
	if a.target != "" {
		target = a.target
	}
	return AdmissionReceipt{Key: r.Event.Key, Owner: owner, AdmissionID: "receipt:" + r.Event.Key.EventID, PayloadHash: input, Response: body, Replacement: &CallbackReplacement{TargetMessageID: target, Token: a.token, ResponseHash: hex.EncodeToString(hash[:])}}, nil
}

func callbackTestCard(t *testing.T, r *Router, id, token string) PlatformMessage {
	t.Helper()
	m := PlatformMessage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BindingGeneration: 1, ChatID: "chat", MessageID: id, Owner: privateOwner, Version: 1, Lifecycle: "active", CardKind: "private_view", CardRef: token}
	if err := r.RecordPublished(t.Context(), m, []byte("platform:"+id), 0); err != nil {
		t.Fatal(err)
	}
	return m
}

func callbackTestEvent(id, message, token string) Event {
	e := testEvent(id, Card)
	e.ChatID = "chat"
	e.MessageID = message
	e.CardKind = "private_view"
	e.CardRef = token
	return e
}

func TestCallbackReplacementSurvivesRestartAndAsyncVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	initial := strings.Repeat("1", 64)
	next := strings.Repeat("2", 64)
	newer := strings.Repeat("3", 64)
	m := callbackTestCard(t, r, "card", initial)
	e := callbackTestEvent("click", "card", initial)
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	record, err := r.Deliver(t.Context(), e.Key, replacementAdmitter{token: next})
	if err != nil {
		t.Fatal(err)
	}
	if current, err := r.CallbackResponseCurrent(t.Context(), record); err != nil || !current {
		t.Fatalf("new response current=%v err=%v", current, err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	old := callbackTestEvent("old-click", "card", initial)
	stale, _, err := r.Admit(t.Context(), old, resolverFunc(classify))
	if err != nil || stale.State != Expired {
		t.Fatalf("old token accepted after restart: %s %v", stale.State, err)
	}
	fresh := callbackTestEvent("fresh-click", "card", next)
	got, _, err := r.Admit(t.Context(), fresh, resolverFunc(classify))
	if err != nil || got.State != Pending {
		t.Fatalf("offered token not restored: %s %v", got.State, err)
	}
	m.Version = 2
	m.CardRef = newer
	if err = r.RecordPublished(t.Context(), m, []byte("platform:card"), 1); err != nil {
		t.Fatal(err)
	}
	if current, err := r.CallbackResponseCurrent(t.Context(), record); err != nil || current {
		t.Fatalf("old response survived real version: %v %v", current, err)
	}
	previous := callbackTestEvent("old-offer", "card", next)
	got, _, err = r.Admit(t.Context(), previous, resolverFunc(classify))
	if err != nil || got.State != Expired {
		t.Fatalf("old offered token survived platform update: %s %v", got.State, err)
	}
}

func TestCallbackResolvedBeforeNewOfferCannotReachApplication(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	initial, next := strings.Repeat("1", 64), strings.Repeat("2", 64)
	callbackTestCard(t, r, "card", initial)
	a, b := callbackTestEvent("a", "card", initial), callbackTestEvent("b", "card", initial)
	for _, event := range []Event{a, b} {
		record, _, err := r.Admit(t.Context(), event, resolverFunc(classify))
		if err != nil || record.State != Pending {
			t.Fatalf("admit %s: %s %v", event.Key.EventID, record.State, err)
		}
	}
	if _, err := r.Deliver(t.Context(), a.Key, replacementAdmitter{token: next}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := r.Deliver(t.Context(), b.Key, replacementAdmitter{token: strings.Repeat("3", 64), calls: &calls})
	if err != nil || calls != 0 || result.State != Expired {
		t.Fatalf("stale resolved callback reached application: state=%s calls=%d err=%v", result.State, calls, err)
	}
}

func TestCallbackOfferDuringAuthorizationInvalidatesOldToken(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	initial, next := strings.Repeat("1", 64), strings.Repeat("2", 64)
	callbackTestCard(t, r, "card", initial)
	a, b := callbackTestEvent("a", "card", initial), callbackTestEvent("b", "card", initial)
	if _, _, err := r.Admit(t.Context(), a, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	type outcome struct {
		record Record
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		record, _, err := r.Admit(context.Background(), b, resolverFunc(func(ctx context.Context, e Event, m *PlatformMessage) (Owner, error) {
			close(entered)
			<-release
			return classify(ctx, e, m)
		}))
		done <- outcome{record, err}
	}()
	<-entered
	if _, err := r.Deliver(t.Context(), a.Key, replacementAdmitter{token: next}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	result := <-done
	if result.err == nil && result.record.State != Expired {
		t.Fatalf("stale token resolved after concurrent replacement: %s", result.record.State)
	}
}

func TestCallbackAdmissionAndAsyncMessageUpdateAreSerialized(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	initial, next := strings.Repeat("1", 64), strings.Repeat("2", 64)
	m := callbackTestCard(t, r, "card", initial)
	e := callbackTestEvent("click", "card", initial)
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	delivery := make(chan error, 1)
	go func() {
		_, err := r.Deliver(context.Background(), e.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
			close(entered)
			<-release
			return json.RawMessage(`{}`), nil
		}))
		delivery <- err
	}()
	<-entered
	m.Version = 2
	m.CardRef = next
	updated := make(chan error, 1)
	go func() { updated <- r.RecordPublished(context.Background(), m, []byte("platform:card"), 1) }()
	select {
	case err := <-updated:
		close(release)
		<-delivery
		t.Fatalf("message receipt overtook in-flight callback admission: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-delivery; err != nil {
		t.Fatal(err)
	}
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
}

func TestCallbackReplacementCannotChangeOwnerOrMessage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owner  *Owner
		target string
	}{{name: "other message", target: "different"}, {name: "other owner", owner: &Owner{Application: Private, ID: "bob", Epoch: 1}}, {name: "other epoch", owner: &Owner{Application: Private, ID: "alice", Epoch: 2}}} {
		t.Run(tc.name, func(t *testing.T) {
			r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
			defer r.Close()
			initial := strings.Repeat("1", 64)
			callbackTestCard(t, r, "card", initial)
			e := callbackTestEvent("click", "card", initial)
			if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Deliver(t.Context(), e.Key, replacementAdmitter{token: strings.Repeat("2", 64), target: tc.target, owner: tc.owner}); err == nil {
				t.Fatal("mismatched receipt accepted")
			}
			var count int
			if err := r.db.QueryRow(`SELECT count(*) FROM private_callback_views`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("injected view persisted: %d %v", count, err)
			}
		})
	}
}

func TestCallbackReplacementRollbackAndRestartReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	initial, next := strings.Repeat("1", 64), strings.Repeat("2", 64)
	callbackTestCard(t, r, "card", initial)
	e := callbackTestEvent("click", "card", initial)
	if _, _, err := r.Admit(t.Context(), e, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`CREATE TRIGGER fail_callback_view BEFORE INSERT ON private_callback_views BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Deliver(t.Context(), e.Key, replacementAdmitter{token: next}); err == nil {
		t.Fatal("injected commit failure accepted")
	}
	var n int
	if err := r.db.QueryRow(`SELECT count(*) FROM private_callback_views`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial callback view: %d %v", n, err)
	}
	if _, err := r.db.Exec(`DROP TRIGGER fail_callback_view`); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	record, err := r.Deliver(t.Context(), e.Key, replacementAdmitter{token: next})
	if err != nil || record.State != Accepted {
		t.Fatalf("replay: %s %v", record.State, err)
	}
	again, err := r.Deliver(t.Context(), e.Key, replacementAdmitter{token: strings.Repeat("3", 64)})
	if err != nil || again.Receipt == nil || again.Receipt.Replacement == nil || again.Receipt.Replacement.Token != next {
		t.Fatalf("committed response changed on replay: %v", err)
	}
}

func TestCallbackUnknownAdmissionReconcilesButCannotOverwriteNewerView(t *testing.T) {
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	initial, next, oldResult := strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)
	callbackTestCard(t, r, "card", initial)
	a, b := callbackTestEvent("new", "card", initial), callbackTestEvent("unknown", "card", initial)
	for _, event := range []Event{a, b} {
		if _, _, err := r.Admit(t.Context(), event, resolverFunc(classify)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Deliver(t.Context(), b.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
		return nil, errors.New("lost application response")
	})); err == nil {
		t.Fatal("unknown application response treated accepted")
	}
	if _, err := r.Deliver(t.Context(), a.Key, replacementAdmitter{token: next}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reconciled, err := r.Deliver(t.Context(), b.Key, replacementAdmitter{token: oldResult, calls: &calls})
	if err != nil || calls != 1 || reconciled.State != Accepted {
		t.Fatalf("exact admission was not reconciled: state=%s calls=%d err=%v", reconciled.State, calls, err)
	}
	if reconciled.Receipt == nil || reconciled.Receipt.Replacement != nil {
		t.Fatal("older response replaced newer view")
	}
	fresh := callbackTestEvent("latest", "card", next)
	got, _, err := r.Admit(t.Context(), fresh, resolverFunc(classify))
	if err != nil || got.State != Pending {
		t.Fatalf("newer view lost: %s %v", got.State, err)
	}
}
