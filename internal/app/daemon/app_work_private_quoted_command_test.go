package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type recordedPrivateCommandHandler struct {
	app     *App
	command work.TypedCommandEnvelope
}

func (h *recordedPrivateCommandHandler) Handle(ctx context.Context, command work.TypedCommandEnvelope) (workingress.PrivateCommandReceipt, error) {
	h.command = command
	return (teamPrivateCommands{app: h.app}).Handle(ctx, command)
}

func TestPrivateQuotedNativeQueryKeepsFrozenOldContextAndCurrentCard(t *testing.T) {
	for _, kind := range []string{"goal", "mcp"} {
		for _, laterNavigation := range []bool{false, true} {
			name := kind + "/keep-front"
			if laterNavigation {
				name = kind + "/reject-later-navigation"
			}
			t.Run(name, func(t *testing.T) {
				f := newPrivateMCPReadFixture(t)
				handler := &recordedPrivateCommandHandler{app: f.a}
				ingress, err := workingress.New(f.a.teamRuntime.store, workingress.Config{Directory: steerSDKDirectory{f.source.Gateway}, PrivateCommands: handler})
				if err != nil {
					t.Fatal(err)
				}
				receive := func(id, text, parent string) {
					t.Helper()
					raw, _ := json.Marshal(map[string]string{"text": text})
					_, err := ingress.Receive(t.Context(), work.RawIngressEnvelope{GatewayRef: f.source.Gateway.GatewayRef, AppID: f.source.Gateway.AppID, EventID: id, Tenant: f.source.Tenant, ChatID: f.source.ChatID, ChatType: "p2p", MessageID: id, ParentID: parent, SenderType: "user", SenderIDs: f.source.SenderIDs, MessageType: "text", RawContent: string(raw), ReceivedAt: time.Now().UTC()})
					if err != nil {
						t.Fatal(err)
					}
					result, err := ingress.ProcessPending(t.Context(), 16)
					if err != nil || len(result.Failures) != 0 {
						t.Fatalf("actual private ingress: %+v %v", result, err)
					}
					if _, err = ingress.DispatchPrivateCommands(t.Context(), 16); err != nil {
						t.Fatal(err)
					}
				}
				receive("quoted-original", "Old authorized work", "")
				var oldContext string
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					nav, err := tx.GetPrivateNavigation(f.source.ScopeID)
					oldContext = nav.FrontContextID
					return err
				})
				moved, err := f.a.teamRuntime.service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "front-new-before-quote", Source: f.source.Authority, Action: "new", ExpectedRevision: f.source.NavigationRevision})
				if err != nil {
					t.Fatal(err)
				}
				receive("quoted-native-query", "/"+kind, "quoted-original")
				command := handler.command
				if command.ContextID != oldContext || command.NavigationRevision != moved.Navigation.Revision || moved.Navigation.FrontContextID == oldContext {
					t.Fatalf("quoted slash lost actual source: %+v", command)
				}
				pending := ""
				commandError := ""
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					saved, err := tx.GetPrivateCommand(command.ID)
					pending = saved.Receipt
					commandError = saved.LastError
					return err
				})
				markPrivateNativeViewPublished(t, f.a, pending)
				if kind == "mcp" {
					if err = f.a.advanceTeamMCPReads(t.Context(), time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
				if len(f.commands) != 1 {
					t.Fatalf("quoted query did not target old native session: text %q command error %q", command.Text, commandError)
				}
				if laterNavigation {
					moved, err = f.a.teamRuntime.service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "front-new-after-query", Source: workstore.PrivateActionSource{PersonalScopeID: command.ScopeID, CommandID: command.ID}, Action: "new", ExpectedRevision: command.NavigationRevision})
					if err != nil {
						t.Fatal(err)
					}
				}
				if kind == "goal" {
					f.goalReply(t, f.commands[0], "old-context-goal")
					err = f.a.advanceTeamGoalUI(t.Context(), time.Now().UTC())
				} else {
					f.respond(t)
					err = f.a.advanceTeamMCPReads(t.Context(), time.Now().UTC())
				}
				if err != nil {
					t.Fatal(err)
				}
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					nav, err := tx.GetPrivateNavigation(f.source.ScopeID)
					if err != nil {
						return err
					}
					if nav != moved.Navigation {
						t.Fatal("quoted query or result unexpectedly switched current front")
					}
					view, err := tx.GetUIView(workui.ID(f.commands[0].CommandID, "result"))
					if laterNavigation {
						if err != workstore.ErrNotFound {
							t.Fatal("late result overwrote newer navigation")
						}
						return nil
					}
					if err != nil {
						return err
					}
					if view.ReplacesViewID != pending || view.ContextID != oldContext {
						t.Fatal("frozen old-context query did not update original current card")
					}
					return nil
				})
			})
		}
	}
}
