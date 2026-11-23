package workstore

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateCodexObservedModelRequiresCommittedStartedTurn(t *testing.T) {
	s, worker, carrier, binding, _ := privateStatusNativeFixture(t, work.BackendCodex)
	for _, tc := range []struct {
		seq           uint64
		model, thread string
		quarantine    bool
		helper        bool
	}{{2, "gpt-5.5", "session", false, false}, {3, "deepseek-v4", "other", false, false}, {4, "deepseek-v4", "session", true, false}, {5, "deepseek-v4", "session", false, true}} {
		event := agentproto.Event{Seq: tc.seq, Kind: agentproto.EventTurnStarted, ThreadID: tc.thread, TurnID: "turn", Execution: &binding, CodexEffectiveThread: &agentproto.CodexEffectiveThreadContract{ConnectionContractID: "account", ModelProviderID: "provider", Model: tc.model}}
		if tc.helper {
			event.TrafficClass = agentproto.TrafficClassInternalHelper
		}
		hash, err := agentproto.RuntimeEventPayloadHash(event)
		if err != nil {
			t.Fatal(err)
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: tc.seq, PayloadHash: hash}
		requireTx(t, s, func(tx *Tx) error {
			_, _, e := tx.AcceptRuntimeEvent(worker.InstanceID, event, time.Now(), nil)
			if e != nil {
				return e
			}
			if tc.quarantine {
				return tx.QuarantineRuntimeEvent(event, "unproven")
			}
			return nil
		})
	}
	requireTx(t, s, func(tx *Tx) error {
		actual, e := tx.PrivateCodexObservedModel(carrier.Incarnation, "session")
		if e != nil {
			return e
		}
		if actual == nil || actual.Model != "gpt-5.5" {
			t.Fatalf("unexpected observed model: %+v", actual)
		}
		return nil
	})
}
