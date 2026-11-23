package workruntime

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivatePreviewSourceRequiresActualHumanAndLaunch(t *testing.T) {
	for _, tc := range []struct {
		name                                                                                      string
		pending, missingLaunch, wrongHost, wrongBot, fakeMessage, missingMessage, wrongGeneration bool
		want                                                                                      bool
	}{
		{name: "actual human and local launch", want: true},
		{name: "pending ingress", pending: true},
		{name: "historical launch absent", missingLaunch: true},
		{name: "other host", wrongHost: true},
		{name: "other bot", wrongBot: true},
		{name: "fabricated message", fakeMessage: true},
		{name: "historical message absent", missingMessage: true},
		{name: "wrong carrier generation", wrongGeneration: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.coordinator.options.EnqueueReconcile = func(string) {}
			f.start()
			cwd := t.TempDir()
			var display workstore.PrivateExecutionDisplay
			f.tx(func(tx *workstore.Tx) error {
				raw, err := tx.GetIngressByPlatformMessage("tenant", "private-chat", "om_user")
				if err != nil {
					return err
				}
				if !tc.pending {
					raw.State = work.IngressProcessed
					raw.MessageID = "private-human-input"
					raw.Revision++
					if err = tx.PutIngress(raw, raw.Revision-1); err != nil {
						return err
					}
				}
				bot, err := tx.GetRoleBotBinding("role")
				if err != nil {
					return err
				}
				display = workstore.PrivateExecutionDisplay{ID: "display", Binding: *f.command.Execution, Incarnation: "inc-1", SessionID: "session", TurnID: "turn-1", Tenant: "tenant", ChatID: "private-chat", ReplyToMessageID: "om_user", Bot: bot}
				if !tc.missingLaunch {
					launch := agentproto.RuntimeLaunchSnapshot{Version: agentproto.WorkerProtocolVersion, Owner: f.carrier.Owner, HostID: "host", CWD: cwd, JournalPath: "/tmp/journal", CarrierIncarnation: "inc-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, InstructionBundle: agentproto.InstructionBundle{Revision: "v1", Text: "instructions"}}
					body, err := json.Marshal(struct {
						Snapshot agentproto.RuntimeLaunchSnapshot `json:"snapshot"`
					}{launch})
					if err != nil {
						return err
					}
					content := work.NewContent("launch-body", "scope", "application/json", body, f.now)
					if err = tx.PutContent(content); err != nil {
						return err
					}
					_, _, err = tx.PutOperation(work.Operation{ID: "launch", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "worker.launch", TargetID: "worker", TargetGeneration: 1, RequestRef: content.ID, RequestHash: content.SHA256, IdempotencyKey: "launch", State: work.OperationPrepared, Revision: 1}, 0)
					if err != nil {
						return err
					}
				}
				return nil
			})
			if tc.wrongBot {
				display.Bot.AppID = "other-app"
			}
			if tc.fakeMessage {
				display.ReplyToMessageID = "om_fabricated"
			}
			if tc.missingMessage {
				display.ReplyToMessageID = ""
			}
			if tc.wrongGeneration {
				display.Binding.SessionGeneration++
			}
			host := "host"
			if tc.wrongHost {
				host = "other-host"
			}
			f.tx(func(tx *workstore.Tx) error {
				source, ok, err := workdisplay.ResolvePreviewSource(tx, display, host)
				if tc.want && (err != nil || !ok || source.ActorOpenID != "review-human" || source.WorkspaceRoot != cwd || source.ThreadCWD != cwd) {
					t.Fatalf("source=%+v ok=%v err=%v", source, ok, err)
				}
				if !tc.want && ok {
					t.Fatalf("unsafe source=%+v err=%v", source, err)
				}
				return nil
			})
		})
	}
}
