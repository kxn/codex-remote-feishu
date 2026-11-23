package workingress

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStopCancelsStagedImagesWithoutConsumingLaterImages(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	ingress.config.Expander = privateImageExpander{}
	old := rawMessage("leader", "image-before-stop", "dm", "p2p", "open-person", "person", "")
	old.MessageType, old.RawContent = "image", `{"image_key":"old"}`
	receive(t, ingress, old)
	process(t, ingress)
	stop := rawMessage("leader", "stop-staged-image", "dm", "p2p", "open-person", "person", "/stop")
	stop.ReceivedAt = old.ReceivedAt.Add(time.Second)
	source, nav := privateCommandSourceFromIngress(t, ingress, store, stop)
	if _, err := privateTestService(t, store).TrustedPrivateStop(t.Context(), workservice.TrustedPrivateStopRequest{Key: "stop-staged-image", Source: source, ExpectedNavRevision: nav.Revision}); err != nil {
		t.Fatal(err)
	}
	newer := rawMessage("leader", "image-after-stop", "dm", "p2p", "open-person", "person", "")
	newer.ReceivedAt = stop.ReceivedAt.Add(time.Second)
	newer.MessageType, newer.RawContent = "image", `{"image_key":"new"}`
	receive(t, ingress, newer)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		all, err := tx.ListInbox(nav.PersonalScopeID)
		if err != nil {
			return err
		}
		for _, in := range all {
			message, err := tx.GetMessage(in.MessageID)
			if err != nil {
				return err
			}
			if message.PlatformKey.MessageID == old.MessageID && (in.State != work.InboxCancelled || !in.AwaitingText) {
				t.Fatalf("stop did not cancel staged image: %+v", in)
			}
		}
		pending, err := tx.ListPendingInputs(nav.PersonalScopeID, "leader")
		if err != nil || len(pending) != 1 || !pending[0].AwaitingText {
			t.Fatalf("new image did not remain staged: %+v %v", pending, err)
		}
		ready, err := tx.MediaBatchHasHumanInstruction(pending)
		if err != nil || ready {
			t.Fatalf("stop or previous input counted as new image instruction: ready=%t %v", ready, err)
		}
		return nil
	})
	caption := rawMessage("leader", "caption-after-stop", "dm", "p2p", "open-person", "person", "解释刚发的图片")
	caption.ReceivedAt = newer.ReceivedAt.Add(time.Second)
	receive(t, ingress, caption)
	process(t, ingress)
	txRun(t, store, func(tx *workstore.Tx) error {
		pending, err := tx.ListPendingInputs(nav.PersonalScopeID, "leader")
		if err != nil || len(pending) != 2 {
			t.Fatalf("caption lost staged image or revived stopped image: %+v %v", pending, err)
		}
		ready, err := tx.MediaBatchHasHumanInstruction(pending)
		if err != nil || !ready {
			t.Fatalf("new image and caption cannot execute: %t %v", ready, err)
		}
		return nil
	})
}
