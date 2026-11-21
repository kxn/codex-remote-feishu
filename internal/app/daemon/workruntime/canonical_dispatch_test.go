package workruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestCanonicalDispatchEnvelopeRejectsWrongCarrierGenerationsAndHash(t *testing.T) {
	for _, mutate := range []string{"instance", "instance-generation", "process-generation", "incarnation", "command-hash", "unknown-field"} {
		t.Run(mutate, func(t *testing.T) {
			f := newFixture(t, work.BackendCodex)
			command := f.command
			command.CommandID = "envelope-negative"
			envelope := map[string]any{"instance_id": f.carrier.OriginInstanceID, "instance_generation": f.carrier.InstanceGeneration, "process_generation": f.carrier.ProcessGeneration, "carrier_incarnation": f.carrier.Incarnation, "command": command}
			rawCommand, _ := json.Marshal(command)
			sum := sha256.Sum256(rawCommand)
			requestHash := hex.EncodeToString(sum[:])
			switch mutate {
			case "instance":
				envelope["instance_id"] = "another-instance"
			case "instance-generation":
				envelope["instance_generation"] = int64(2)
			case "process-generation":
				envelope["process_generation"] = int64(2)
			case "incarnation":
				envelope["carrier_incarnation"] = "another-incarnation"
			case "command-hash":
				requestHash = "wrong"
			case "unknown-field":
				envelope["uncontracted_owner"] = true
			}
			f.tx(func(tx *workstore.Tx) error {
				raw, _ := json.Marshal(envelope)
				body := work.NewContent("negative-envelope", "scope", "application/json", raw, f.now)
				if err := tx.PutContent(body); err != nil {
					return err
				}
				_, _, err := tx.PutOperation(work.Operation{ID: command.CommandID, ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "native.dispatch", TargetID: "worker", TargetGeneration: 1, RequestRef: body.ID, RequestHash: requestHash, IdempotencyKey: command.CommandID, State: work.OperationSending, Revision: 1}, 0)
				return err
			})
			if err := f.coordinator.RegisterCommand(context.Background(), f.carrier.Incarnation, command); err == nil {
				t.Fatal("canonical dispatch silently accepted " + mutate)
			}
		})
	}
}
