package daemon

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateReactionShellSuccessDoesNotEchoPayloadOrBecomeModelFinal(t *testing.T) {
	for _, mode := range []string{"normal", "quiet", "published-unknown"} {
		t.Run(mode, func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			var mu sync.Mutex
			var cards []string
			patches := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "tenant_access_token"):
					fmt.Fprint(w, `{"code":0,"tenant_access_token":"shell-test","expire":7200}`)
				case r.URL.Path == "/callback/ws/endpoint":
					fmt.Fprint(w, `{"code":1}`)
				case (r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages") || (r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/open-apis/im/v1/messages/")):
					raw, _ := io.ReadAll(r.Body)
					mu.Lock()
					cards = append(cards, string(raw))
					if r.Method == http.MethodPatch {
						patches++
						if r.URL.Path != "/open-apis/im/v1/messages/shell-card-1" {
							t.Errorf("replacement escaped original notice: %s", r.URL.Path)
						}
					}
					number := len(cards)
					mu.Unlock()
					fmt.Fprintf(w, `{"code":0,"data":{"message_id":"shell-card-%d","chat_id":"private-chat"}}`, number)
				default:
					t.Errorf("unexpected output route %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			useDeliveryJourneyGateway(t, f.a, server.URL, f.source.Gateway)
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				scope, proof, err := tx.ValidatePrivateAction(f.source.Authority)
				if err != nil {
					return err
				}
				return tx.PutContextPreferences(work.ContextPreferences{UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Source: proof, ScopeID: f.late.ScopeID, ContextID: f.late.RecipientContextID, Verbose: func() *string {
					value := mode
					if value == "published-unknown" {
						value = "normal"
					}
					return &value
				}(), Revision: 1}, 0, f.source.Authority)
			})
			actionID := f.reaction(t, "actual-shell-reaction", "APPLAUSE", f.source.SenderIDs, time.Now())
			f.relay.send = func(_ string, command agentproto.Command) error {
				if command.Kind != agentproto.CommandThreadShellCommand {
					t.Fatal("unexpected native operation")
				}
				if command.ShellCommand.OperationID == "" || command.NativeInputAdmission != nil || command.Execution == nil || command.Execution.AttemptID != "" || command.Execution.RunRef != "" {
					t.Fatal("session shell was disguised as model input")
				}
				receiptStatus := "acknowledged"
				if mode == "quiet" || mode == "published-unknown" {
					receiptStatus = "unknown"
				}
				if err := f.event(agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: command.CommandID, RequestID: "shell-rpc", ThreadID: command.Target.ThreadID, Execution: command.Execution, Status: receiptStatus}); err != nil {
					return err
				}

				if mode == "published-unknown" {
					for range 4 {
						if _, e := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16); e != nil {
							return e
						}
					}
				}

				translator := codex.NewTranslator("native-output")
				result, err := translator.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"steer-session","turnId":"independent-shell-turn","item":{"id":"actual-shell-item","type":"commandExecution","source":"userShell","status":"completed","exitCode":0,"aggregatedOutput":"actual native stdout","stderr":"actual native stderr"}}}`))
				if err != nil {
					return err
				}
				if len(result.Events) != 1 {
					return fmt.Errorf("unexpected native output events")
				}
				// The wrapper's exact fixed-command/item binding supplies this ID; its
				// independent runtime test rejects unrelated or out-of-order items.
				event := result.Events[0]
				event.CommandID = command.CommandID
				event.Kind = agentproto.EventNativeShellItemObserved
				event.Action = "completed"
				event.Metadata["shellOperationID"] = command.ShellCommand.OperationID
				event.Metadata["userShellOutput"] = command.ShellCommand.Payload
				delete(event.Metadata, "userShellStderr")
				event.Execution = command.Execution
				if err = f.event(event); err != nil {
					return err
				}
				if err = f.event(event); err != nil {
					return err
				}
				// Strong completion cannot be downgraded by late weak RPC facts.
				for _, status := range []string{"unknown", "failed"} {
					if err = f.event(agentproto.Event{Kind: agentproto.EventNativeShellRPCResult, CommandID: command.CommandID, RequestID: "shell-rpc", ThreadID: command.Target.ThreadID, Execution: command.Execution, Status: status, ErrorMessage: "late failure after actual delivery"}); err != nil {
						return err
					}
				}
				return nil
			}
			if err := f.a.runTeamPrivateReactionActions(t.Context()); err != nil {
				t.Fatal(err)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				action, e := tx.GetPrivateReactionAction(actionID)
				if e != nil {
					return e
				}
				if action.LastError != "" {
					t.Fatalf("shell action rejected: %s", action.LastError)
				}
				return nil
			})
			for range 8 {
				n, err := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if mode == "published-unknown" && (len(cards) != 2 || patches != 1 || !strings.Contains(cards[0], "尚未确认") || !strings.Contains(cards[1], "已确认追加")) {
				t.Fatalf("notice did not converge on its original card: %v", cards)
			}
			for _, card := range cards {
				for _, forbidden := range []string{"原生命令结果", "actual native stdout", "actual native stderr", "queued_input_bundle", "Late authorized work", "未被接收", "待核对", "late failure"} {
					if strings.Contains(card, forbidden) && !(mode == "published-unknown" && forbidden == "待核对") {
						t.Fatalf("successful APPLAUSE echoed payload or added an unsolicited result card: %q", forbidden)
					}
				}
				if strings.Contains(card, `reply_in_thread`) {
					t.Fatal("shell feedback escaped private chat")
				}
			}

			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				input, err := tx.GetInbox(f.late.ID)
				if err != nil {
					return err
				}
				if input.State != work.InboxProcessed || input.ClaimedAttemptID != "" || input.ShellOperationID == "" {
					t.Fatal("actual independent shell did not settle its own input")
				}
				ids, err := tx.EffectiveAttemptInputIDs("steer-attempt")
				if err != nil {
					return err
				}
				if len(ids) != 1 {
					t.Fatalf("shell entered model EffectiveInputIDs: %v", ids)
				}
				attempt, err := tx.GetAttempt("steer-attempt")
				if err != nil {
					return err
				}
				if attempt.State != work.AttemptRunning {
					t.Fatal("shell ended the original model turn")
				}
				shell, err := tx.GetPrivateShellOperation(input.ShellOperationID)
				if err != nil {
					return err
				}
				if shell.State != "completed" || shell.ActualTurnID != "independent-shell-turn" {
					t.Fatalf("shell did not preserve actual independent turn: %+v", shell)
				}
				if _, err = tx.GetNativeTurn(shell.Incarnation, shell.SessionID, shell.ActualTurnID); err != workstore.ErrNotFound {
					t.Fatal("shell actual turn was registered as a model turn")
				}
				operations, e := tx.ListOperations(f.late.ScopeID)
				if e != nil {
					return e
				}
				for _, op := range operations {
					if op.Kind != "feishu.publish" {
						continue
					}
					projected, e := workservice.ReadMessageProjection(tx, op)
					if e != nil {
						return e
					}
					if projected.PrivateDisplay == nil || projected.PrivateDisplay.DisplayID == "" {
						continue
					}
					display, e := tx.GetPrivateExecutionDisplay(projected.PrivateDisplay.DisplayID)
					if e != nil {
						return e
					}
					if display.Terminated || display.PendingFinal {
						t.Fatal("shell result terminated or replaced model final")
					}
				}
				return nil
			})
		})
	}
}
