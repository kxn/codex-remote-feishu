package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type historicalNativeComponentEvidence struct {
	Migration ingressowner.ImportProof               `json:"migration"`
	Command   workstore.NativeCommand                `json:"command"`
	Carrier   workstore.CarrierRegistration          `json:"carrier"`
	Process   agentproto.RuntimeProcessIdentity      `json:"process"`
	Inventory runtimejournal.LegacyInventoryEvidence `json:"inventory"`
}

func matchesHistoricalCommandCarrier(command workstore.NativeCommand, carrier workstore.CarrierRegistration, process agentproto.RuntimeProcessIdentity, scope, host string) bool {
	return carrier.Incarnation == command.Incarnation && carrier.Owner.SameOwner(command.Binding) && carrier.Owner.ScopeID == scope && process.HostID == host && host != "" && process.PID > 0 && process.StartIdentity != "" && process.Platform != ""
}

// WithVerifiedMigrationHold authorizes only the source-wide barrier here.
// Each remaining source-operation hold requires its own reconciliation proof.
func (v teamSealedImportVerifier) WithVerifiedMigrationHold(ctx context.Context, req privatechat.MigrationImportRequest, receipt privatechat.MigrationImportReceipt, hold orchestrator.PrivateRecoveryHold, commit func(json.RawMessage) error) error {
	if (hold.Kind != "migration_source_unverified" && hold.Kind != "source_native_commands") || req.SourceHandle == "" || receipt.ImportID != req.ImportID || receipt.Owner != req.Owner || receipt.Generation != req.Generation || receipt.SourceScope != v.binding.ScopeID {
		return privatechat.ErrRecoveryHeld
	}
	var seal workstore.PersonalMigrationSeal
	if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(v.binding.ScopeID, req.SourceHandle)
		return e
	}); err != nil {
		return err
	}
	if seal.SourceHash != receipt.SourceHash || migrationBytesHash(seal.Evidence) != receipt.ProofHash {
		return privatechat.ErrJournalConflict
	}
	var source PersonalMigrationSource
	var frozen struct {
		SourceHash string `json:"source_hash"`
		Reason     string `json:"reason"`
	}
	var evidence personalSourceSealEvidence
	var component *historicalNativeComponentEvidence
	if json.Unmarshal(seal.Manifest, &source) != nil || json.Unmarshal(seal.Evidence, &evidence) != nil {
		return privatechat.ErrJournalConflict
	}
	if hold.Kind == "migration_source_unverified" && (json.Unmarshal(hold.Frozen, &frozen) != nil || frozen.SourceHash != source.SHA256 || frozen.Reason != "drain_and_final_export_proof_required") {
		return privatechat.ErrJournalConflict
	}
	if hold.Kind == "source_native_commands" {
		var command workstore.NativeCommand
		if json.Unmarshal(hold.Frozen, &command) != nil || command.CommandID == "" || command.Binding.ScopeID != seal.ScopeID {
			return privatechat.ErrJournalConflict
		}
		found := 0
		for _, row := range source.Source.Records {
			if row.Table == "native_commands" && row.Key == command.CommandID && bytes.Equal(row.Payload, hold.Frozen) {
				found++
			}
		}
		if found != 1 {
			return privatechat.ErrRecoveryHeld
		}
		var carrier workstore.CarrierRegistration
		var process agentproto.RuntimeProcessIdentity
		var carrierCount, processCount int
		for _, row := range source.Source.Records {
			if row.Key != command.Incarnation {
				continue
			}
			switch row.Table {
			case "carrier_registrations":
				var wrapped struct {
					Registration workstore.CarrierRegistration `json:"registration"`
				}
				if json.Unmarshal(row.Payload, &wrapped) != nil {
					return privatechat.ErrJournalConflict
				}
				carrier = wrapped.Registration
				carrierCount++
			case "runtime_carrier_processes":
				if json.Unmarshal(row.Payload, &process) != nil {
					return privatechat.ErrJournalConflict
				}
				processCount++
			}
		}
		if carrierCount != 1 || processCount != 1 || !matchesHistoricalCommandCarrier(command, carrier, process, seal.ScopeID, v.app.teamRuntime.codexUpgradeHostID) {
			return privatechat.ErrRecoveryHeld
		}
		var snapshot agentproto.RuntimeLaunchSnapshot
		var sourceReceipt agentproto.Event
		if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(command.CommandID)
			if err != nil {
				return err
			}
			if op.ScopeID != seal.ScopeID || (op.State != work.OperationConfirmed && op.State != work.OperationTerminal) {
				return privatechat.ErrRecoveryHeld
			}
			actualCarrier, err := tx.GetCarrier(command.Incarnation)
			if err != nil {
				return err
			}
			actualProcess, err := tx.GetCarrierProcess(command.Incarnation)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(actualCarrier, carrier) || !reflect.DeepEqual(actualProcess, process) {
				return privatechat.ErrJournalConflict
			}
			snapshot, err = tx.GetNativeLaunchSnapshot(command.Binding.WorkerSessionID, command.Binding.SessionGeneration)
			if err != nil {
				return err
			}
			if snapshot.CarrierIncarnation != command.Incarnation || !snapshot.Owner.SameOwner(command.Binding) || snapshot.HostID != process.HostID {
				return privatechat.ErrJournalConflict
			}
			if command.SessionID != "" {
				sourceReceipt, err = tx.LegacyCarrierSessionReceipt(seal.ScopeID, command.Binding.WorkerSessionID, command.Incarnation, command.SessionID)
				if errors.Is(err, workstore.ErrNotFound) {
					err = nil
				}
				if err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		err := hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity})
		cancel()
		if err != nil {
			return privatechat.ErrRecoveryHeld
		}
		inventory, err := runtimejournal.VerifyLegacyRetiredInventory(ctx, snapshot, v.app.headlessRuntime.Paths.StateDir)
		if err != nil && sourceReceipt.Journal != nil {
			inventory, err = runtimejournal.VerifyLegacyRetiredInventoryWithReceipt(ctx, snapshot, v.app.headlessRuntime.Paths.StateDir, command.SessionID, sourceReceipt)
		}
		if err != nil || !inventory.ProcessScopesStopped {
			return privatechat.ErrRecoveryHeld
		}
		component = &historicalNativeComponentEvidence{Command: command, Carrier: carrier, Process: process, Inventory: inventory}
	}
	proof := ingressowner.ImportProof{Drain: evidence.Neutral, SourceSnapshotHash: seal.SourceHash, Target: v.target, TransactionID: req.ImportID}
	return v.router.WithCommittedOwnerMigration(ctx, proof, func() error {
		if err := v.app.verifyPersonalSourceSealLocal(ctx, seal, v.limits); err != nil {
			return err
		}
		var value any = proof
		if component != nil {
			component.Migration = proof
			value = component
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return commit(raw)
	})
}
