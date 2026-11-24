package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPersonalExecutionHoldDoesNotCoverOlderAttemptOrOccupancy(t *testing.T) {
	s, _ := openTestStore(t)
	now := time.Now().UTC()
	err := s.Within(t.Context(), func(tx *Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "p", DisplayName: "p", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "r", Name: "r", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "s", Kind: work.ScopePersonal, PrincipalID: "p", RoleID: "r", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "s", RoleID: "r", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "w", ContextID: "context", ScopeID: "s", RoleID: "r", Backend: work.BackendCodex, State: work.WorkerRecovering, Generation: 2, Controller: work.ControllerAutomatic, ControllerGeneration: 2, InstanceID: "instance-new", InstanceGeneration: 2, ProcessGeneration: 2, CarrierIncarnation: "carrier-new", Revision: 1}
		if e := tx.PutWorker(worker, 0); e != nil {
			return e
		}
		wr, _ := json.Marshal(worker)
		process := agentproto.RuntimeProcessIdentity{HostID: "host", Platform: "linux", PID: 123, StartIdentity: "start"}
		pr, _ := json.Marshal(process)
		if _, e := tx.tx.ExecContext(tx.ctx, "INSERT INTO carrier_registrations(incarnation,worker_id,instance_id,payload) VALUES(?,?,?,?)", "carrier-new", "w", "instance-new", "{}"); e != nil {
			return e
		}
		if _, e := tx.tx.ExecContext(tx.ctx, "INSERT INTO runtime_carrier_processes(incarnation,instance_id,payload) VALUES(?,?,?)", "carrier-new", "instance-new", string(pr)); e != nil {
			return e
		}
		f, e := tx.BeginPersonalMigrationFence("s", "migration", now)
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain("s", "migration", f.Revision, now); e != nil {
			return e
		}
		proof := work.NewContent("proof", "s", "application/json", []byte("{\"digest\":\"p\"}"), now)
		if e = tx.PutContent(proof); e != nil {
			return e
		}
		evidence, _ := json.Marshal(PersonalExecutionIsolation{Worker: worker, Process: process, ProcessScopesStopped: true})
		frozen, _ := json.Marshal([]json.RawMessage{wr, pr, evidence, json.RawMessage(proof.Data)})
		sum := sha256.Sum256(frozen)
		if _, e = tx.tx.ExecContext(tx.ctx, "INSERT INTO personal_migration_execution_holds(scope_id,worker_id,worker_revision,worker_payload,process_payload,intake_proof_id,evidence,evidence_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?)", "s", "w", 1, string(wr), string(pr), "proof", string(evidence), hex.EncodeToString(sum[:]), now.Format(time.RFC3339Nano)); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "old", ContextID: "context", ScopeID: "s", RunRef: work.RunRef{WorkerID: "w", Generation: 1, RunID: "old-run"}, State: work.AttemptRunning, CommandID: "old-command", Revision: 1}
		ar, _ := json.Marshal(attempt)
		if _, e = tx.tx.ExecContext(tx.ctx, "INSERT INTO attempts(id,revision,payload) VALUES(?,?,?)", "old", 1, string(ar)); e != nil {
			return e
		}
		if _, e = tx.tx.ExecContext(tx.ctx, "INSERT INTO carrier_occupancy(instance_id,instance_generation,process_generation,attempt_id) VALUES(?,?,?,?)", "instance-old", 1, 1, "old"); e != nil {
			return e
		}
		if _, e = tx.tx.ExecContext(tx.ctx, "INSERT INTO resource_occupancy(resource_key,attempt_id,generation) VALUES(?,?,?)", "resource", "old", 1); e != nil {
			return e
		}
		status, e := tx.PersonalMigrationStatus("s")
		if e != nil {
			return e
		}
		if status.HeldExecutionAttempts != 0 || status.UnsettledAttempts != 1 || status.OccupiedCarriers != 1 || status.OccupiedResources != 1 {
			t.Fatalf("old generation hidden: %+v", status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
