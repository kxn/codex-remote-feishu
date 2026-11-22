package workstore

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStatusNativeSettingsRequireCommittedCurrentServerObservation(t *testing.T) {
	for _, scenario := range []string{"claude-server", "codex-server", "codex-client-only", "no-receipt", "wrong-session", "wrong-generation", "wrong-controller", "wrong-bot", "changed-bot-binding", "wrong-carrier", "wrong-instance", "quarantine", "rollback", "bad-hash"} {
		t.Run(scenario, func(t *testing.T) {
			backend := work.BackendClaude
			if scenario == "codex-server" || scenario == "codex-client-only" {
				backend = work.BackendCodex
			}
			s, worker, carrier, binding, _ := privateStatusNativeFixture(t, backend)
			var event agentproto.Event
			if backend == work.BackendClaude {
				result, e := claude.NewTranslator(worker.InstanceID).ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"plan","model":"actual"}`))
				if e != nil || len(result.Events) != 1 {
					t.Fatalf("actual Claude init translation: %v %#v", e, result.Events)
				}
				event = result.Events[0]
			} else {
				translator := codex.NewTranslator(worker.InstanceID)
				if scenario == "codex-client-only" {
					result, e := translator.ObserveClient([]byte(`{"method":"turn/start","params":{"threadId":"session","approvalPolicy":"never","sandbox":"danger-full-access"}}`))
					if e != nil || len(result.Events) == 0 {
						t.Fatal(e)
					}
					event = result.Events[0]
				} else {
					result, e := translator.ObserveServer([]byte(`{"method":"thread/settings/updated","params":{"threadId":"session","settings":{"approvalPolicy":"on-request","sandbox":"workspace-write"}}}`))
					if e != nil || len(result.Events) != 1 {
						t.Fatal(e)
					}
					event = result.Events[0]
				}
			}
			event.Execution = agentproto.CloneExecutionBinding(&binding)
			switch scenario {
			case "wrong-session":
				event.ThreadID = "old-session"
			case "wrong-generation":
				event.Execution.SessionGeneration++
			case "wrong-controller":
				event.Execution.ControllerGeneration++
			case "wrong-bot":
				event.Execution.BotGeneration++
			}
			event.Seq = 3
			incarnation := carrier.Incarnation
			if scenario == "wrong-carrier" {
				incarnation = "old-carrier"
			}
			hash, e := agentproto.RuntimeEventPayloadHash(event)
			if e != nil {
				t.Fatal(e)
			}
			event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: incarnation, Sequence: 3, PayloadHash: hash}
			at := time.Date(2026, 11, 22, 14, 1, 2, 0, time.UTC)
			instance := worker.InstanceID
			if scenario == "wrong-instance" {
				instance = "old-instance"
			}
			if scenario != "no-receipt" {
				sentinel := errors.New("rollback")
				err := s.Within(t.Context(), func(tx *Tx) error {
					_, _, e := tx.AcceptRuntimeEvent(instance, event, at, nil)
					if e != nil {
						return e
					}
					if scenario == "rollback" {
						return sentinel
					}
					if scenario == "quarantine" {
						return tx.QuarantineRuntimeEvent(event, "unproven")
					}
					return nil
				})
				if scenario == "rollback" {
					if !errors.Is(err, sentinel) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "changed-bot-binding" {
				requireTx(t, s, func(tx *Tx) error {
					return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "changed", IdentityGeneration: 2}, 1)
				})
			}
			if scenario == "bad-hash" {
				requireTx(t, s, func(tx *Tx) error {
					_, e := tx.tx.ExecContext(tx.ctx, "UPDATE runtime_receipts SET payload_hash='changed' WHERE carrier_incarnation=?", carrier.Incarnation)
					return e
				})
			}
			requireTx(t, s, func(tx *Tx) error {
				observed, e := tx.ReadPrivateNativeSettings(worker)
				if scenario == "bad-hash" {
					if e == nil {
						t.Fatal("changed committed proof accepted")
					}
					return nil
				}
				if e != nil {
					return e
				}
				positive := scenario == "claude-server" || scenario == "codex-server"
				if positive {
					if observed.Permission == nil || observed.Permission.Receipt.PayloadHash != hash || observed.Permission.ObservedAt != at {
						t.Fatal("actual server permission lost receipt provenance")
					}
					if scenario == "claude-server" && (observed.Plan == nil || observed.Plan.Event.PlanMode != "on") {
						t.Fatal("actual native plan mode omitted")
					}
					if scenario == "codex-server" && observed.Plan != nil {
						t.Fatal("invented native plan observation")
					}
				} else if observed.Permission != nil || observed.Plan != nil {
					t.Fatal("unproven or client-only settings shown as native observation")
				}
				return nil
			})
		})
	}
}
