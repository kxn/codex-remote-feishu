package ingressowner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type legacyBarrierFixture struct{}

func (legacyBarrierFixture) VerifyLegacyScopeBarrier(_ context.Context, b LegacyScopeBarrier) error {
	if b.ScopeID != "old" || b.RoleID != "role" {
		return errors.New("not source evidence")
	}
	return nil
}
func TestLegacyBarrierBlocksFreshPrivateButNotGroupOrOtherBot(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "neutral.db")
	r := openTest(t, path)
	r.SetLegacyScopeVerifier(legacyBarrierFixture{})
	b := LegacyScopeBarrier{ScopeID: "old", RoleID: "role", AppID: "app", GatewayID: "gateway", BotGeneration: 1, Reason: "missing_sdk_identity"}
	if err := r.RecordLegacyScopeBarrier(ctx, LegacyScopeBarrier{ScopeID: "forged", RoleID: "role", AppID: "app", GatewayID: "gateway", BotGeneration: 1, Reason: b.Reason}); err == nil {
		t.Fatal("forged source accepted")
	}
	if err := r.RecordLegacyScopeBarrier(ctx, b); err != nil {
		t.Fatal(err)
	}
	private := testEvent("private", Message)
	private.ChatType = "p2p"
	private.ChatID = "chat"
	private.MessageID = "message"
	private.Payload = json.RawMessage(`{"sender_type":"user","sender_tenant":"tenant"}`)
	resolve := resolverFunc(func(ctx context.Context, e Event, _ *PlatformMessage) (Owner, error) {
		binding, err := r.EnsureObservedPrivateBinding(ctx, e)
		return binding.Owner, err
	})
	got, _, err := r.Admit(ctx, private, resolve)
	if err != nil || got.State != Unresolved {
		t.Fatalf("private was not retained: %+v %v", got, err)
	}
	group := testEvent("group", Message)
	group.ChatType = "group"
	group.ChatID = "group"
	group.MessageID = "group-message"
	got, _, err = r.Admit(ctx, group, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) { return teamOwner, nil }))
	if err != nil || got.State != Pending {
		t.Fatalf("group blocked: %+v %v", got, err)
	}
	other := private
	other.Key.EventID = "other"
	other.Key.AppID = "another-app"
	other.Source.GatewayID = "other-gateway"
	other.MessageID = "other-message"
	got, _, err = r.Admit(ctx, other, resolve)
	if err != nil || got.State != Pending || got.Owner == nil || got.Owner.Application != Private {
		t.Fatalf("other bot blocked: %+v %v", got, err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	r.SetLegacyScopeVerifier(legacyBarrierFixture{})
	list, err := r.ListLegacyScopeBarriers(ctx, "", 10)
	if err != nil || len(list) != 1 || list[0] != b {
		t.Fatal("barrier lost on restart")
	}
	_, err = r.EnsureObservedPrivateBinding(ctx, private)
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("restart allowed fresh private: %v", err)
	}
}
