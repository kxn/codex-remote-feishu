package workruntime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateAnswerIgnoresProvenMenuTransportAndMCPReadOnlyRequests(t *testing.T) {
	for _, kind := range []string{"pending-menu", "unknown-menu-and-mcp-read", "business-publication", "unregistered-ui-publication"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			f.tx(func(tx *workstore.Tx) error {
				scope, err := tx.GetScope("scope")
				if err != nil {
					return err
				}
				binding, err := tx.GetRoleBotBinding("role")
				if err != nil {
					return err
				}
				if kind == "pending-menu" || kind == "unknown-menu-and-mcp-read" {
					card, err := workui.Card("pending-private-menu", workui.Panel{Title: "工作菜单", Buttons: []workui.Button{{Label: "状态", Action: "private.status"}}})
					if err != nil {
						return err
					}
					view, err := workui.Queue(tx, workstore.UIView{ID: "pending-private-menu", ScopeID: scope.ID, ContextID: "context", SurfaceKey: "private-navigation", Kind: "private.navigation", TargetID: scope.ID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "private-chat", PrincipalID: scope.PrincipalID, ReplyToMessageID: "om_user"}, card)
					if err != nil {
						return err
					}
					if kind == "unknown-menu-and-mcp-read" {
						operation, err := tx.GetOperation(view.OperationID)
						if err != nil {
							return err
						}
						for _, state := range []work.OperationState{work.OperationSending, work.OperationUnknown} {
							before := operation.Revision
							operation.Revision++
							operation.State = state
							if _, _, err := tx.PutOperation(operation, before); err != nil {
								return err
							}
						}
						outbox, err := tx.GetOutbox(view.ID + ":outbox")
						if err != nil {
							return err
						}
						for _, state := range []work.OutboxState{work.OutboxSending, work.OutboxUnknown} {
							before := outbox.Revision
							outbox.Revision++
							outbox.State = state
							if err := tx.PutOutbox(outbox, before); err != nil {
								return err
							}
						}

						owner := f.carrier.Owner
						command := agentproto.Command{CommandID: "pending-private-mcp", Kind: agentproto.CommandRuntimeMCPRead, Execution: &owner, Target: agentproto.Target{ThreadID: "session"}, RuntimeMCP: &agentproto.RuntimeMCPQuery{Version: agentproto.RuntimeMCPProtocolVersion, Mode: "configuration"}}
						raw, err := json.Marshal(command)
						if err != nil {
							return err
						}
						body := work.NewContent("pending-private-mcp:request", scope.ID, "application/json", raw, f.now)
						if err := tx.PutContent(body); err != nil {
							return err
						}
						_, _, err = tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: scope.ID, ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "user-session-mcp"}, Kind: "native.user.mcp.read", TargetID: owner.WorkerSessionID, TargetGeneration: owner.SessionGeneration, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}, 0)
						return err
					}
					return nil
				}
				actor := work.Actor{Kind: work.ActorRole, ID: "role"}
				projection := workservice.MessageProjection{RoleID: binding.RoleID, GatewayRef: binding.GatewayRef, AppID: binding.AppID, BotGeneration: binding.IdentityGeneration, Tenant: "tenant", ChatID: "private-chat", Body: "unresolved business result"}
				if kind == "unregistered-ui-publication" {
					actor = work.Actor{Kind: work.ActorSystem, ID: "team-ui"}
					projection.UIViewID = "nonexistent-view"
					projection.Card = json.RawMessage(`{"schema":"2.0","body":{"elements":[]}}`)
				}
				raw, err := json.Marshal(projection)
				if err != nil {
					return err
				}
				body := work.NewContent("unresolved-publication:request", scope.ID, "application/json", raw, f.now)
				if err := tx.PutContent(body); err != nil {
					return err
				}
				_, _, err = tx.PutOperation(work.Operation{ID: "unresolved-publication", ScopeID: scope.ID, ContextID: "context", Actor: actor, Kind: "feishu.publish", TargetID: "private-chat", TargetGeneration: 1, IdempotencyKey: "unresolved-publication", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationUnknown, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}, 0)
				return err
			})
			privateFinalEvent(f, "actual ordinary private answer during read-only menu activity")
			err := completePrivateTurn(f)
			if kind == "pending-menu" || kind == "unknown-menu-and-mcp-read" {
				if err != nil {
					t.Fatal(err)
				}
				assertPrivateSucceeded(t, f, 1)
			} else {
				if err == nil {
					t.Fatal("an unproven publication was mistaken for transport-only UI")
				}
				f.tx(func(tx *workstore.Tx) error {
					if _, err := tx.GetProcessingReport("attempt"); !errors.Is(err, workstore.ErrNotFound) {
						t.Fatalf("unresolved business publication allowed processing: %v", err)
					}
					return nil
				})
			}
		})
	}
}
