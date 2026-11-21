package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestSharingGrantPinsAdmittedHumanAndExactTarget(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	entry := memory("published-note", work.MemoryScope{Kind: work.MemoryRole, RoleID: "r"})
	grants := grantSharing(t, s, []work.SharingTarget{memorySharingTarget(entry)})
	entry.SharingGrantID = grants[entry.ID]
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutMemory(entry, 0); err != nil {
			return err
		}
		grant, err := tx.GetSharingGrant("s", entry.SharingGrantID)
		if err != nil {
			return err
		}
		forged := grant
		forged.ID = "forged"
		forged.SourceInboxID = "not-admitted"
		if err = tx.PutSharingGrant(forged, 0); err == nil {
			t.Fatal("old/unadmitted human message supplied sharing authority")
		}
		forged = grant
		forged.ID = "wrong-actor"
		forged.AuthorizedBy = work.Actor{Kind: work.ActorRole, ID: "r"}
		if err = tx.PutSharingGrant(forged, 0); err == nil {
			t.Fatal("role self-published private source")
		}
		changed := entry
		changed.ID = "another-note"
		if err = tx.PutMemory(changed, 0); err == nil {
			t.Fatal("grant reused for another object")
		}
		return nil
	})
	requireTx(t, s, func(tx *Tx) error {
		grant, err := tx.GetSharingGrant("s", entry.SharingGrantID)
		if err != nil {
			return err
		}
		grant.State = work.SharingRevoked
		grant.Revision++
		if err = tx.PutSharingGrant(grant, 1); err != nil {
			return err
		}
		if _, _, err = tx.ReadMemory(work.ReadScope{ScopeID: "s", RoleID: "r"}, entry.ID); err == nil {
			t.Fatal("revoked grant still exposes body")
		}
		return nil
	})
}
