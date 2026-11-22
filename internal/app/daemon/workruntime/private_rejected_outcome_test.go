package workruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestLegacySuppressedControlFailureIsPublishedDuringReconciliation(t *testing.T) {
	f := newFixture(t, work.BackendCodex, false, true, true)
	f.start()
	event := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "command", Execution: f.command.Execution, ThreadID: "session", TurnID: "turn-1", Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Problem: &agentproto.ErrorInfo{Code: "modelNotFound", Message: "legacy model not supported"}})
	// Persist exactly the old runtime's terminal fact without a display: earlier
	// versions suppressed every PurposeControl event, including real failures.
	f.tx(func(tx *workstore.Tx) error {
		_, _, err := tx.AcceptRuntimeEvent(f.carrier.OriginInstanceID, event, f.now, func(tx *workstore.Tx) error {
			turn, err := tx.GetNativeTurn(f.carrier.Incarnation, "session", "turn-1")
			if err != nil {
				return err
			}
			turn.TerminalStatus, turn.TerminalSequence, turn.LastSequence = "failed", event.Seq, event.Seq
			return tx.PutNativeTurn(turn)
		})
		return err
	})
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		input := codexInspection(request)
		input.Native.Response = json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","result":{"thread":{"id":%q,"status":{"type":"systemError"},"turns":[{"id":%q,"status":"failed","items":[{"type":"userMessage"}],"error":{"codexErrorInfo":"modelNotFound"}}]}}}`, request.SessionID, request.TurnID))
		return ObserveProtocol(request, input)
	}
	for range 2 {
		if err := f.coordinator.Reconcile(t.Context(), "attempt"); err != nil {
			t.Fatal(err)
		}
		f.tx(func(tx *workstore.Tx) error {
			attempt, err := tx.GetAttempt("attempt")
			if err != nil {
				return err
			}
			ops, err := tx.PrivateRunPublications(attempt.ScopeID, attempt.ContextID, attempt.RunRef.RunID)
			if err != nil {
				return err
			}
			count := 0
			for _, op := range ops {
				projection, err := workservice.ReadMessageProjection(tx, op)
				if err != nil {
					return err
				}
				if strings.Contains(string(projection.Card), "legacy model not supported") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("legacy failure display count=%d", count)
			}
			return nil
		})
	}
}

func TestPrivateModelRejectionEndsFailedRequestWithoutManualReconciliation(t *testing.T) {
	for _, control := range []bool{false, true} {
		t.Run(fmt.Sprint("control=", control), func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true, control)
			f.start()
			f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
				input := codexInspection(request)
				input.Native.Response = json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","result":{"thread":{"id":%q,"status":{"type":"systemError"},"turns":[{"id":%q,"status":"failed","items":[{"type":"userMessage"}],"error":{"codexErrorInfo":"modelNotFound"}}]}}}`, request.SessionID, request.TurnID))
				return ObserveProtocol(request, input)
			}
			err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "command", ThreadID: "session", TurnID: "turn-1", Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Problem: &agentproto.ErrorInfo{Code: "modelNotFound", Message: "所选模型不支持当前账号"}})
			if err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				attempt, err := tx.GetAttempt("attempt")
				if err != nil {
					return err
				}
				if attempt.State != work.AttemptFailed {
					t.Fatalf("attempt=%+v", attempt)
				}
				input, err := tx.GetInbox("input")
				if err != nil {
					return err
				}
				if input.State != work.InboxProcessed {
					t.Fatalf("failed request still repeats: %+v", input)
				}
				if _, err = tx.GetRuntimeGate("worker"); !errors.Is(err, workstore.ErrNotFound) {
					t.Fatalf("known rejection kept manual gate: %v", err)
				}
				if control {
					original, err := tx.GetInbox("original-control-input")
					if err != nil {
						return err
					}
					if original.State != work.InboxPending {
						t.Fatal("failed management consumed original business input")
					}
				}
				_, err = tx.GetQuiescenceReceipt("attempt")
				return err
			})
		})
	}
}

func TestPrivateModelRejectionCannotHideExecutedToolOrUnknownBackground(t *testing.T) {
	for _, kind := range []string{"tool", "background"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex, false, true)
			f.start()
			f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
				input := codexInspection(request)
				item := "userMessage"
				if kind == "tool" {
					item = "commandExecution"
				}
				input.Native.Response = json.RawMessage(fmt.Sprintf(`{"id":"inspect-native","result":{"thread":{"id":%q,"status":{"type":"systemError"},"turns":[{"id":%q,"status":"failed","items":[{"type":%q}],"error":{"codexErrorInfo":"modelNotFound"}}]}}}`, request.SessionID, request.TurnID, item))
				if kind == "background" {
					input.Background.Complete = false
				}
				return ObserveProtocol(request, input)
			}
			_ = f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: "command", ThreadID: "session", TurnID: "turn-1", Status: "failed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime, Problem: &agentproto.ErrorInfo{Code: "modelNotFound", Message: "model unavailable"}})
			f.tx(func(tx *workstore.Tx) error {
				input, err := tx.GetInbox("input")
				if err != nil {
					return err
				}
				if input.State == work.InboxProcessed {
					t.Fatal("uncertain effects treated as no execution")
				}
				if _, err = tx.GetRuntimeGate("worker"); err != nil {
					return err
				}
				return nil
			})
		})
	}
}
