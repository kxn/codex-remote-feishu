package workstore

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateProjectIdentityUsesRealScopedAliases(t *testing.T) {
	s, _ := openTestStore(t)
	first := PrivateProjectIdentityObservation{Tenant: "tenant", AppID: "app1", EventID: "event1", FactHash: "hash1", OwnerID: "private1", OwnerEpoch: 1, Generation: 1, IDs: work.SenderIDs{OpenID: "same-open", UserID: "tenant-user"}}
	var principal work.Principal
	requireTx(t, s, func(tx *Tx) error { var e error; principal, e = tx.ResolvePrivateProjectIdentity(first); return e })
	second := PrivateProjectIdentityObservation{Tenant: "tenant", AppID: "app2", EventID: "event2", FactHash: "hash2", OwnerID: "private2", OwnerEpoch: 1, Generation: 1, IDs: work.SenderIDs{OpenID: "other-open", UserID: "tenant-user"}}
	requireTx(t, s, func(tx *Tx) error {
		p, e := tx.ResolvePrivateProjectIdentity(second)
		if e == nil && p.ID != principal.ID {
			t.Fatal("tenant user identity not linked")
		}
		return e
	})
	separate := PrivateProjectIdentityObservation{Tenant: "tenant", AppID: "app3", EventID: "event3", FactHash: "hash3", OwnerID: "private3", OwnerEpoch: 1, Generation: 1, IDs: work.SenderIDs{OpenID: "same-open"}}
	requireTx(t, s, func(tx *Tx) error {
		p, e := tx.ResolvePrivateProjectIdentity(separate)
		if e == nil && p.ID == principal.ID {
			t.Fatal("open ID guessed across apps")
		}
		return e
	})
	forged := first
	forged.FactHash = "different"
	requireTx(t, s, func(tx *Tx) error {
		if _, e := tx.ResolvePrivateProjectIdentity(forged); e == nil {
			t.Fatal("changed frozen SDK event accepted")
		}
		return nil
	})
	// An earlier app-only observation is retained when a later real contact
	// response establishes a tenant identity already known through another bot.
	appOnly := PrivateProjectIdentityObservation{Tenant: "tenant", AppID: "late-app", EventID: "old", FactHash: "old-hash", OwnerID: "late-owner", OwnerEpoch: 1, Generation: 1, IDs: work.SenderIDs{OpenID: "late-open"}}
	requireTx(t, s, func(tx *Tx) error { _, e := tx.ResolvePrivateProjectIdentity(appOnly); return e })
	confirmed := appOnly
	confirmed.EventID, confirmed.FactHash = "new", "new-hash"
	confirmed.IDs.UserID = "tenant-user"
	confirmed.ContactOpenID, confirmed.ContactUserID = "late-open", "tenant-user"
	requireTx(t, s, func(tx *Tx) error {
		if _, e := tx.ResolvePrivateProjectIdentity(confirmed); e == nil {
			t.Fatal("conflicting histories silently merged")
		}
		confirmed.AliasIDs = work.SenderIDs{UserID: "tenant-user"}
		got, e := tx.ResolvePrivateProjectIdentity(confirmed)
		if e != nil || got.ID != principal.ID {
			t.Fatalf("verified tenant identity: %+v %v", got, e)
		}
		return nil
	})
}
