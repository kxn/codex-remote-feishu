package workingress

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type replyPatchSender struct {
	replyPublisher
	failPatch bool
}

func (p replyPatchSender) Resolve(context.Context, work.RoleBotBinding) (workpublish.Sender, error) {
	return p, nil
}
func (p replyPatchSender) PatchCard(_ context.Context, r feishu.ProjectCardPatchRequest) (feishu.ProjectPublishReceipt, error) {
	if p.failPatch {
		return feishu.ProjectPublishReceipt{}, errors.New("actual patch response lost")
	}
	return feishu.ProjectPublishReceipt{MessageID: r.MessageID, ChatID: r.ChatID}, nil
}

func TestPrivateReplyRejectsCrossContextOrUnconfirmedInlineCard(t *testing.T) {
	for _, state := range []string{"same_context_confirmed", "cross_context_confirmed", "unknown_patch"} {
		t.Run(state, func(t *testing.T) {
			i, s, _ := ingressFixture(t, false)
			source, nav := privateCommandSource(t, i, s, "source")
			var old workstore.UIView
			txRun(t, s, func(tx *workstore.Tx) error {
				binding, e := tx.GetRoleBotBinding("leader")
				if e != nil {
					return e
				}
				card, e := workui.Card("reply-card", workui.Panel{Title: "Original"})
				if e != nil {
					return e
				}
				old, e = workui.Queue(tx, workstore.UIView{ID: "reply-card", ScopeID: nav.PersonalScopeID, ContextID: nav.FrontContextID, SurfaceKey: "reply-card", Kind: "test-result", TargetID: nav.FrontContextID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "dm", PrincipalID: nav.PrincipalID}, card)
				return e
			})
			publisher, e := workpublish.New(s, replyPatchSender{failPatch: state == "unknown_patch"}, func() time.Time { return ingressNow })
			if e != nil {
				t.Fatal(e)
			}
			if _, e = publisher.RunOnce(t.Context(), 10); e != nil {
				t.Fatal(e)
			}
			newer, e := privateTestService(t, s).TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "next", Source: source, Action: "new", ExpectedRevision: nav.Revision})
			if e != nil {
				t.Fatal(e)
			}
			txRun(t, s, func(tx *workstore.Tx) error {
				card, e := workui.Card("replacement", workui.Panel{Title: "Replacement"})
				if e != nil {
					return e
				}
				contextID := old.ContextID
				if state == "cross_context_confirmed" {
					contextID = newer.Navigation.FrontContextID
				}
				_, e = workui.Queue(tx, workstore.UIView{ID: "replacement", ReplacesViewID: old.ID, ScopeID: old.ScopeID, ContextID: contextID, SurfaceKey: old.SurfaceKey, Kind: old.Kind, TargetID: contextID, TargetRevision: 1, Binding: old.Binding, Tenant: old.Tenant, ChatID: old.ChatID, PrincipalID: old.PrincipalID}, card)
				return e
			})
			_, e = publisher.RunOnce(t.Context(), 10)
			if state == "unknown_patch" {
				txRun(t, s, func(tx *workstore.Tx) error {
					v, err := tx.GetUIView("replacement")
					if err != nil {
						return err
					}
					op, err := tx.GetOperation(v.OperationID)
					if err == nil && (op.State != work.OperationUnknown || op.LastError != "actual patch response lost") {
						t.Fatalf("missing real uncertain patch result: %+v", op)
					}
					return err
				})
			} else if e != nil {
				t.Fatal(e)
			}
			raw := rawMessage("leader", "quoted-updated-card", "dm", "p2p", "open-person", "person", "Continue this")
			raw.ParentID = "bot-old"
			receipt := receive(t, i, raw)
			txRun(t, s, func(tx *workstore.Tx) error {
				saved, e := tx.GetIngress(receipt.IngressID)
				if e != nil {
					return e
				}
				blocked := saved.Route.ReplyRoutingError != ""
				if blocked != (state != "same_context_confirmed") {
					t.Fatalf("wrong quoted card proof result state=%s route=%+v", state, saved.Route)
				}
				return nil
			})
		})
	}
}

func TestPrivateReplyAuthorityAllowsSameGenerationMetadataEnrichment(t *testing.T) {
	i, s, d := ingressFixture(t, false)
	_, nav := privateCommandSource(t, i, s, "source")
	receive(t, i, rawMessage("leader", "original-human", "dm", "p2p", "open-person", "person", "old work"))
	process(t, i)
	gateway := d.gateways["leader"]
	gateway.BotOpenID = "newly-observed-bot-open"
	gateway.UnionDomain = "actual-domain"
	d.gateways["leader"] = gateway
	raw := rawMessage("leader", "same-generation-reply", "dm", "p2p", "open-person", "person", "continue")
	raw.ParentID = "original-human"
	receipt := receive(t, i, raw)
	txRun(t, s, func(tx *workstore.Tx) error {
		saved, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		if saved.Route.ReplyRoutingError != "" || saved.Route.ContextID != nav.FrontContextID {
			t.Fatal("metadata enrichment broke exact same Bot authority")
		}
		return nil
	})
}

func TestQuotedPrivateStopTargetsFrozenOldWorkWithoutMovingView(t *testing.T) {
	i, s, _ := ingressFixture(t, false)
	source, nav := privateCommandSource(t, i, s, "source")
	receive(t, i, rawMessage("leader", "old-human", "dm", "p2p", "open-person", "person", "old work"))
	process(t, i)
	service := privateTestService(t, s)
	newer, e := service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "new-view", Source: source, Action: "new", ExpectedRevision: nav.Revision})
	if e != nil {
		t.Fatal(e)
	}
	raw := rawMessage("leader", "quoted-stop", "dm", "p2p", "open-person", "person", "/stop")
	raw.ParentID = "old-human"
	receipt := receive(t, i, raw)
	process(t, i)
	var command work.TypedCommandEnvelope
	txRun(t, s, func(tx *workstore.Tx) error {
		commands, e := tx.ListPendingPrivateCommands()
		if e != nil {
			return e
		}
		for _, candidate := range commands {
			if candidate.IngressID == receipt.IngressID {
				command = candidate
				before := command.Revision
				command.Revision++
				command.State = "sending"
				return tx.PutPrivateCommand(command, before)
			}
		}
		return workstore.ErrNotFound
	})
	if _, e = service.TrustedPrivateStop(t.Context(), workservice.TrustedPrivateStopRequest{Key: "quoted-stop", Source: workstore.PrivateActionSource{PersonalScopeID: command.ScopeID, CommandID: command.ID}, ExpectedNavRevision: command.NavigationRevision}); e != nil {
		t.Fatal(e)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		current, e := tx.GetPrivateNavigation(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		if current.FrontContextID != newer.Navigation.FrontContextID {
			t.Fatal("stop changed navigation")
		}
		gates, e := tx.ListPauseGates(nav.PersonalScopeID)
		if e != nil {
			return e
		}
		found := false
		for _, gate := range gates {
			if gate.State == work.GateClosed {
				found = true
				if gate.ContextID != nav.FrontContextID {
					t.Fatal("quoted stop paused unrelated current work")
				}
			}
		}
		if !found {
			t.Fatal("old quoted context was not paused")
		}
		return nil
	})
}

func TestQuotedPrivateReplyRechecksExpiringProjectAccessBeforeModel(t *testing.T) {
	i, s, _ := ingressFixture(t, true)
	receive(t, i, rawMessage("leader", "membership", "group", "group", "open-person", "person", "hello"))
	process(t, i)
	source, nav := privateCommandSource(t, i, s, "private-source")
	if _, e := privateTestService(t, s).TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "enter-project", Source: source, Action: "enter_project", ProjectScopeID: "scope", ExpectedRevision: nav.Revision}); e != nil {
		t.Fatal(e)
	}
	receive(t, i, rawMessage("leader", "project-private-anchor", "dm", "p2p", "open-person", "person", "project discussion"))
	process(t, i)
	raw := rawMessage("leader", "late-membership-reply", "dm", "p2p", "open-person", "person", "continue project work")
	raw.ParentID = "project-private-anchor"
	receipt := receive(t, i, raw)
	i.config.Now = func() time.Time { return ingressNow.Add(48 * time.Hour) }
	result, e := i.ProcessPending(t.Context(), 32)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.BlockedReplies) != 1 || result.BlockedReplies[0] != receipt.IngressID {
		t.Fatalf("expired access still admitted quote to model: %+v", result)
	}
	txRun(t, s, func(tx *workstore.Tx) error {
		saved, e := tx.GetIngress(receipt.IngressID)
		if e != nil {
			return e
		}
		if saved.ReplyDeliveryError == "" || saved.MessageID != "" {
			t.Fatal("processing-time membership failure was not durably retained without model input")
		}
		return nil
	})
}
