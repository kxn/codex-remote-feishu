package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestPrivateCaptionClusterExcludesPreviousRequestAndTrailingPictures(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = privateImageExpander{}
	var scope string
	inputs := map[string]work.Inbox{}
	for index, name := range []string{"old-image", "old-caption", "image-one", "image-two", "caption", "trailing-image"} {
		raw := rawMessage("leader", name, "private-chat", "p2p", "open", "user", name)
		raw.ReceivedAt = time.Date(2026, 11, 23, 1, 0, index, 0, time.UTC)
		if name != "old-caption" && name != "caption" {
			raw.MessageType = "image"
			raw.RawContent = `{"image_key":"opaque"}`
		}
		receipt := receive(t, ingress, raw)
		process(t, ingress)
		txRun(t, store, func(tx *workstore.Tx) error {
			record, e := tx.GetIngress(receipt.IngressID)
			if e != nil {
				return e
			}
			scope = record.Route.ScopeID
			all, e := tx.ListInbox(scope)
			if e != nil {
				return e
			}
			for _, in := range all {
				if in.MessageID == record.MessageID {
					inputs[name] = in
				}
			}
			return nil
		})
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		// A historical request remains a cluster boundary after it leaves pending.
		old := inputs["old-caption"]
		before := old.Revision
		old.Revision++
		old.State = work.InboxCancelled
		if e := tx.PutInbox(old, before); e != nil {
			return e
		}
		cluster, e := tx.PendingInputMediaCluster(inputs["caption"])
		if e != nil {
			return e
		}
		want := []string{"image-one", "image-two", "caption"}
		if len(cluster) != len(want) {
			t.Fatalf("cluster length %d", len(cluster))
		}
		for i, name := range want {
			if cluster[i].ID != inputs[name].ID {
				t.Fatalf("cluster[%d] lost exact source ordering", i)
			}
		}
		return nil
	})
}
