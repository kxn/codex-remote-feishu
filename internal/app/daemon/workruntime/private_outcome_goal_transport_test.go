package workruntime

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateGoalReadAndUIWrapperDoNotHideUncertainMutations(t *testing.T) {
	for _, kind := range []string{"get", "get-old-session", "set", "clear", "intent-ui"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			f.tx(func(tx *workstore.Tx) error {
				scope, err := tx.GetScope("scope")
				if err != nil {
					return err
				}
				if kind == "intent-ui" {
					gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
					raw := work.RawIngressEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "goal-authority-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "goal-authority-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "review-human"}, MessageType: "text", RawContent: `{"text":"/goal new --budget 200 deliberate next work"}`, ReceivedAt: f.now}
					ingress, _, err := tx.AcceptRawIngress(raw, gateway)
					if err != nil {
						return err
					}
					if err := tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "goal-ui-authority", IngressID: ingress.ID, ScopeID: scope.ID, ContextID: "context", PrincipalID: scope.PrincipalID, RoleID: "role", GatewayRef: "gateway", Text: "/goal new --budget 200 deliberate next work", State: "sending", Revision: 1}, 0); err != nil {
						return err
					}
					budget := int64(200)
					intent, _, err := tx.QueueSessionGoalIntent("pending-goal-intent", "context", agentproto.GoalCommand{Objective: "deliberate next work", TokenBudget: &budget}, workstore.PrivateActionSource{PersonalScopeID: scope.ID, CommandID: "goal-ui-authority"}, f.now)
					if err != nil {
						return err
					}
					binding, err := tx.GetRoleBotBinding(scope.RoleID)
					if err != nil {
						return err
					}
					card, err := workui.Card("pending-goal-view", workui.Panel{Title: "会话目标", Paragraphs: []string{"正在等待原生会话反馈"}})
					if err != nil {
						return err
					}
					view, err := workui.Queue(tx, workstore.UIView{ID: "pending-goal-view", ScopeID: scope.ID, ContextID: "context", SurfaceKey: "private-navigation", Kind: "private.navigation", TargetID: scope.ID, TargetRevision: 1, Binding: binding, Tenant: "tenant", ChatID: "private-chat", PrincipalID: scope.PrincipalID, ReplyToMessageID: raw.MessageID}, card)
					if err != nil {
						return err
					}
					payload, _ := json.Marshal(intent.ID)
					body := work.NewContent(intent.ID+":ui:request", scope.ID, "application/json", payload, f.now)
					if err := tx.PutContent(body); err != nil {
						return err
					}
					_, _, err = tx.PutOperation(work.Operation{ID: intent.ID + ":ui", ScopeID: scope.ID, ContextID: intent.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "session-goal-ui"}, Kind: "private.goal.intent-ui", TargetID: intent.ID, TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: intent.ID + ":ui", State: work.OperationSending, Steps: []work.OperationStep{{ID: "ui_pending", State: work.OperationConfirmed, Receipt: view.ID}}, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}, 0)
					return err
				}
				command := f.command
				command.CommandID = "pending-goal-command"
				command.Kind = agentproto.CommandThreadGoalGet
				if kind == "get-old-session" {
					worker, err := tx.GetWorker("worker")
					if err != nil {
						return err
					}
					worker.ID, worker.InstanceID, worker.CarrierIncarnation, worker.NativeSessionID, worker.State = "previous-worker", "previous-instance", "previous-carrier", "previous-session", work.WorkerRetired
					worker.Revision = 1
					if err := tx.PutWorker(worker, 0); err != nil {
						return err
					}
					previousOwner := *command.Execution
					previousOwner.WorkerSessionID = worker.ID
					previousOwner.AttemptID, previousOwner.RunRef = "", ""
					command.Execution = &previousOwner
					command.Target.ThreadID = worker.NativeSessionID
				}

				if kind == "set" {
					command.Kind = agentproto.CommandThreadGoalSet
				} else if kind == "clear" {
					command.Kind = agentproto.CommandThreadGoalClear
				}
				payload, _ := json.Marshal(command)
				body := work.NewContent("pending-goal-command:request", scope.ID, "application/json", payload, f.now)
				if err := tx.PutContent(body); err != nil {
					return err
				}
				_, _, err = tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: scope.ID, ContextID: "context", Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "native.user.goal", TargetID: command.Execution.WorkerSessionID, TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: command.CommandID, State: work.OperationUnknown, Revision: 1, CreatedAt: f.now, UpdatedAt: f.now}, 0)
				return err
			})
			privateFinalEvent(f, "ordinary private answer while goal UI is pending")
			err := completePrivateTurn(f)
			if kind == "get" || kind == "get-old-session" || kind == "intent-ui" {
				if err != nil {
					t.Fatal(err)
				}
				assertPrivateSucceeded(t, f, 1)
				if kind == "intent-ui" {
					f.tx(func(tx *workstore.Tx) error {
						intent, err := tx.GetSessionGoalIntent("pending-goal-intent")
						if err != nil {
							return err
						}
						input, err := tx.GetInbox(intent.InputID)
						if err != nil {
							return err
						}
						if input.State != work.InboxPending {
							t.Fatalf("unadmitted goal was silently consumed: %+v", input)
						}
						return nil
					})
				}
			} else if err == nil {
				t.Fatal("unconfirmed GoalSet/GoalClear was mistaken for a read-only query")
			}
		})
	}
}
