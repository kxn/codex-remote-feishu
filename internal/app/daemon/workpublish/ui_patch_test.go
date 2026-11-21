package workpublish

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

type patchTestSender struct {
	cardTestSender
	patches int
	fail    bool
}

func (s *patchTestSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	return s, nil
}
func (s *patchTestSender) PublishCard(ctx context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	receipt, err := s.cardTestSender.PublishCard(ctx, r)
	if s.calls > 1 {
		receipt.MessageID = "om_recovery"
	}
	return receipt, err
}
func (s *patchTestSender) PatchCard(_ context.Context, r feishu.ProjectCardPatchRequest) (feishu.ProjectPublishReceipt, error) {
	s.patches++
	if r.MessageID != "om_receipt" {
		return feishu.ProjectPublishReceipt{}, errors.New("wrong message")
	}
	if s.fail {
		return feishu.ProjectPublishReceipt{}, errors.New("response lost")
	}
	return feishu.ProjectPublishReceipt{ChatID: r.ChatID, MessageID: r.MessageID}, nil
}
func TestNavigationPatchKeepsMessageAndUnknownNeverOverwritesNewerView(t *testing.T) {
	store, _, _, _ := publisherFixture(t)
	sender := &patchTestSender{}
	p, err := New(store, sender, nil)
	if err != nil {
		t.Fatal(err)
	}
	queue := func(id, old string) {
		t.Helper()
		err = store.Within(context.Background(), func(tx *workstore.Tx) error {
			v := workstore.UIView{ID: id, ReplacesViewID: old, ScopeID: "scope", SurfaceKey: "menu", Kind: "private.navigation", TargetID: "project", TargetRevision: 1, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", ChannelGeneration: 1}
			card, e := workui.Card(id, workui.Panel{Title: id})
			if e != nil {
				return e
			}
			if _, e = workui.Queue(tx, v, card); e != nil {
				return e
			}
			_, e = workui.Queue(tx, v, card)
			return e // exact retry after prior view invalidation
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	run := func() {
		t.Helper()
		if _, err = p.RunOnce(context.Background(), 10); err != nil {
			t.Fatal(err)
		}
	}
	queue("first", "")
	run()
	queue("second", "first")
	run()
	if sender.calls != 1 || sender.patches != 1 {
		t.Fatalf("creates=%d patches=%d", sender.calls, sender.patches)
	}
	sender.fail = true
	queue("unknown", "second")
	run()
	if err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation("unknown:publish")
		if e != nil {
			return e
		}
		if op.State != work.OperationUnknown {
			t.Fatal("lost response not retained as unknown")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := sender.patches
	if err = p.RetryUnknown(context.Background(), "unknown:outbox"); err == nil {
		t.Fatal("unsafe patch replay allowed")
	}
	if sender.patches != before {
		t.Fatal("unknown patch was replayed")
	}
	queue("recovery", "")
	run()
	if sender.calls != 2 {
		t.Fatal("no fresh visible recovery entry")
	}
}
