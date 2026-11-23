package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateShellExactPreparedZeroAckReleasesOnlyUnwrittenSource(t *testing.T) {
	for _, frames := range []int{0, 1} {
		t.Run(map[int]string{0: "prepared-zero", 1: "possible-write"}[frames], func(t *testing.T) {
			f := newReactionActionFixture(t, work.BackendCodex)
			var mu sync.Mutex
			var cards []string
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "tenant_access_token"):
					fmt.Fprint(w, `{"code":0,"tenant_access_token":"ack-test","expire":7200}`)
				case r.URL.Path == "/callback/ws/endpoint":
					fmt.Fprint(w, `{"code":1}`)
				case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
					raw, _ := io.ReadAll(r.Body)
					mu.Lock()
					requests++
					number := requests
					if strings.Contains(string(raw), "鼓掌追加") {
						cards = append(cards, string(raw))
					}
					mu.Unlock()
					fmt.Fprintf(w, `{"code":0,"data":{"message_id":"ack-notice-%d","chat_id":"private-chat"}}`, number)
				default:
					t.Errorf("unexpected ACK notice HTTP %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			useDeliveryJourneyGateway(t, f.a, server.URL, f.source.Gateway)
			f.reaction(t, "native-prewrite-shell-reject", "APPLAUSE", f.source.SenderIDs, time.Now())
			var commandID string
			f.relay.send = func(instance string, command agentproto.Command) error {
				commandID = command.CommandID
				raw, _ := json.Marshal(command)
				sum := sha256.Sum256(raw)
				coordinator := workruntime.New(f.a.teamRuntime.store, workruntime.Options{})
				ack := agentproto.CommandAck{CommandID: commandID, Accepted: false, Error: "active turn ended before translation", RuntimeReceipt: &agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "steer-carrier", CommandID: commandID, PayloadHash: hex.EncodeToString(sum[:]), Stage: "prepared", PreparedFrames: frames, WrittenFrames: frames}}
				if e := coordinator.HandleAck(t.Context(), instance, ack); e != nil {
					return e
				}
				if e := coordinator.HandleAck(t.Context(), instance, ack); e != nil {
					return e
				}
				if frames > 0 {
					ack.RuntimeReceipt.PreparedFrames = 0
					ack.RuntimeReceipt.WrittenFrames = 0
					for replay := 0; replay < 2; replay++ {
						if e := coordinator.HandleAck(t.Context(), instance, ack); e != nil {
							return e
						}
					}
				}
				return nil
			}
			if e := f.a.runTeamPrivateReactionActions(t.Context()); e != nil {
				t.Fatal(e)
			}
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				op, e := tx.GetPrivateShellOperation(commandID)
				if e != nil {
					return e
				}
				input, e := tx.GetInbox(f.late.ID)
				if e != nil {
					return e
				}
				if _, e = tx.GetRuntimeGate(op.Execution.WorkerSessionID); !errors.Is(e, workstore.ErrNotFound) {
					t.Fatal("owner-only ACK created a model recovery gate")
				}
				if frames == 0 {
					if op.State != "rejected" || op.ReceiptRef == "" || input.ShellOperationID != "" || input.State != work.InboxPending {
						t.Fatal("proven unsent rejection stranded original source")
					}
				} else if op.State != "unknown" || input.ShellOperationID != op.ID || input.State != work.InboxPending {
					t.Fatal("possible native write was released or consumed")
				}
				attempt, e := tx.GetAttempt(op.FrozenExecution.AttemptID)
				if e != nil {
					return e
				}
				if attempt.State != work.AttemptRunning {
					t.Fatal("shell ACK mutated model attempt")
				}
				return nil
			})
			for range 4 {
				if _, e := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16); e != nil {
					t.Fatal(e)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			expected := "未被接收"
			if frames > 0 {
				expected = "待核对"
			}
			if len(cards) != 1 || !strings.Contains(cards[0], expected) {
				t.Fatalf("ACK replay did not produce one precise notice: %v", cards)
			}
		})
	}
}
