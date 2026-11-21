package workingress

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type replyPublisher struct{}

func (p replyPublisher) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return p, nil
}
func (replyPublisher) PublishText(_ context.Context, r feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error) {
	return feishu.ProjectPublishReceipt{MessageID: "bot-old", ChatID: r.ChatID}, nil
}
func (replyPublisher) PublishCard(_ context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	return feishu.ProjectPublishReceipt{MessageID: "bot-old", ChatID: r.ChatID}, nil
}

func TestPrivateReplyUsesActualSourceDespiteNavigation(t *testing.T) {
	for _, anchor := range []string{"human-old", "bot-old"} {
		t.Run(anchor, func(t *testing.T) {
			i, s, _ := ingressFixture(t, false)
			source, nav := privateCommandSource(t, i, s, "command")
			receive(t, i, rawMessage("leader", "human-old", "dm", "p2p", "open-person", "person", "original work"))
			process(t, i)
			if anchor == "bot-old" {
				txRun(t, s, func(tx *workstore.Tx) error {
					binding, e := tx.GetRoleBotBinding("leader")
					if e != nil {
						return e
					}
					card, e := workui.Card("old-card", workui.Panel{Title: "Original result"})
					if e != nil {
						return e
					}
					_, e = workui.Queue(tx, workstore.UIView{ID: "old-card", ScopeID: nav.PersonalScopeID, ContextID: nav.FrontContextID, SurfaceKey: "old", Kind: "test-result", TargetID: nav.FrontContextID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "dm", PrincipalID: nav.PrincipalID}, card)
					return e
				})
				publisher, e := workpublish.New(s, replyPublisher{}, func() time.Time { return ingressNow })
				if e != nil {
					t.Fatal(e)
				}
				if n, e := publisher.RunOnce(t.Context(), 10); e != nil || n != 1 {
					t.Fatalf("actual publication: %d %v", n, e)
				}
			}
			view, e := privateTestService(t, s).TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "new", Source: source, Action: "new", ExpectedRevision: nav.Revision})
			if e != nil {
				t.Fatal(e)
			}
			env := rawMessage("leader", "reply", "dm", "p2p", "open-person", "person", "change item three")
			env.ParentID = anchor
			receipt := receive(t, i, env)
			process(t, i)
			txRun(t, s, func(tx *workstore.Tx) error {
				raw, e := tx.GetIngress(receipt.IngressID)
				if e != nil {
					return e
				}
				if raw.Route.ContextID != nav.FrontContextID || raw.Route.ReplyRoutingError != "" {
					t.Fatalf("wrong frozen reply route: %+v", raw.Route)
				}
				current, e := tx.GetPrivateNavigation(nav.PersonalScopeID)
				if e != nil {
					return e
				}
				if current != view.Navigation {
					t.Fatal("reply unexpectedly changed navigation")
				}
				inputs, e := tx.ListInbox(nav.PersonalScopeID)
				if e != nil {
					return e
				}
				found := false
				for _, input := range inputs {
					if input.MessageID == raw.MessageID {
						found = true
						if input.RecipientContextID != nav.FrontContextID {
							t.Fatal("reply was delivered to new context")
						}
					}
				}
				if !found {
					t.Fatal("reply was not admitted")
				}
				return nil
			})
		})
	}
}

func TestPrivateReplyUnknownParentCannotFallBackToKnownRoot(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	_, nav := privateCommandSource(t, i, s, "command")
	receive(t, i, rawMessage("leader", "known-root", "dm", "p2p", "open-person", "person", "old work"))
	process(t, i)
	env := rawMessage("leader", "unknown-reply", "dm", "p2p", "open-person", "person", "approve it")
	env.ParentID = "unknown-parent"
	env.RootID = "known-root"
	receipt := receive(t, i, env)
	result, e := i.ProcessPending(t.Context(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.BlockedReplies) != 1 || result.BlockedReplies[0] != receipt.IngressID {
		t.Fatalf("missing explicit route notice: %+v", result)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		raw, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		if raw.Route.ReplyRoutingError == "" || raw.MessageID != "" {
			t.Fatal("unproven reply reached model")
		}
		inputs, e := tx.ListInbox(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			t.Fatalf("unknown reply created executable input: %d", len(inputs))
		}
		return nil
	})
}
