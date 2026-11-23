package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type LegacyInventoryEvidence struct {
	LaunchHash, JournalLaunchHash, SourceReceiptHash, InventoryHash                                                          string
	NativeCommandsIdle, InputsIdle, ToolsIdle, GoalsIdle, TasksIdle, TurnsTerminal, UncertaintiesEmpty, ProcessScopesStopped bool
}

// VerifyLegacyRetiredInventory proves the exact frozen journal and rechecks
// each controlled OS process scope. It reports each native domain separately;
// process exit alone is never promoted into an idle/terminal assertion.
func VerifyLegacyRetiredInventory(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir string) (LegacyInventoryEvidence, error) {
	return verifyLegacyRetiredInventory(ctx, snapshot, stateDir, "", nil)
}

// VerifyLegacyRetiredInventoryWithReceipt permits a differing legacy launch
// fingerprint only with the same independently persisted source/journal event.
func VerifyLegacyRetiredInventoryWithReceipt(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir, session string, receipt agentproto.Event) (LegacyInventoryEvidence, error) {
	return verifyLegacyRetiredInventory(ctx, snapshot, stateDir, session, &receipt)
}

func verifyLegacyRetiredInventory(ctx context.Context, snapshot agentproto.RuntimeLaunchSnapshot, stateDir, session string, receipt *agentproto.Event) (LegacyInventoryEvidence, error) {
	var result LegacyInventoryEvidence
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return result, err
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		return result, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return result, err
	}
	result.LaunchHash = hash(raw)
	var stored string
	if err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.CarrierIncarnation).Scan(&stored); err != nil {
		return result, err
	}
	result.JournalLaunchHash = stored
	if stored != result.LaunchHash {
		if receipt == nil || receipt.Journal == nil || receipt.Execution == nil || session == "" || receipt.ThreadID != session || receipt.Journal.CarrierIncarnation != snapshot.CarrierIncarnation || !receipt.Execution.SameOwner(snapshot.Owner) {
			return result, ErrConflict
		}
		actual, e := agentproto.RuntimeEventPayloadHash(*receipt)
		if e != nil || actual != receipt.Journal.PayloadHash {
			return result, ErrConflict
		}
		var eventHash string
		var payload []byte
		if e = tx.QueryRowContext(ctx, `SELECT hash,payload FROM events WHERE incarnation=? AND sequence=?`, snapshot.CarrierIncarnation, receipt.Journal.Sequence).Scan(&eventHash, &payload); e != nil {
			return result, e
		}
		var journalEvent agentproto.Event
		if json.Unmarshal(payload, &journalEvent) != nil {
			return result, ErrConflict
		}
		journalHash, e := agentproto.RuntimeEventPayloadHash(journalEvent)
		if e != nil || journalHash != eventHash || eventHash != actual {
			return result, ErrConflict
		}
		result.SourceReceiptHash = actual
	}
	var inventory []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM inventory_state WHERE incarnation=?`, snapshot.CarrierIncarnation).Scan(&inventory); err != nil {
		return result, err
	}
	result.InventoryHash = hash(inventory)
	var v struct {
		Owner                agentproto.ExecutionBinding                `json:"owner"`
		Scopes               []hostinventory.Snapshot                   `json:"scopes"`
		ActiveNativeCommands map[string]bool                            `json:"active_native_commands"`
		PendingInputs        map[string]string                          `json:"pending_inputs"`
		ActiveTools          map[string]bool                            `json:"active_tools"`
		GoalActive           map[string]bool                            `json:"goal_active"`
		GoalKnown            map[string]bool                            `json:"goal_known"`
		SessionIDs           map[string]bool                            `json:"session_ids"`
		Tasks                map[string]bool                            `json:"tasks"`
		Uncertainties        map[string]bool                            `json:"uncertainties"`
		TaskInventoryKnown   bool                                       `json:"task_inventory_known"`
		Turns                map[string]struct{ Active, Terminal bool } `json:"turns"`
	}
	if json.Unmarshal(inventory, &v) != nil || !v.Owner.SameOwner(snapshot.Owner) {
		return result, ErrConflict
	}
	noTrue := func(values map[string]bool) bool {
		for _, v := range values {
			if v {
				return false
			}
		}
		return true
	}
	result.NativeCommandsIdle = v.ActiveNativeCommands != nil && noTrue(v.ActiveNativeCommands)
	result.InputsIdle = v.PendingInputs != nil && len(v.PendingInputs) == 0
	result.ToolsIdle = v.ActiveTools != nil && noTrue(v.ActiveTools)
	result.GoalsIdle = v.GoalActive != nil && v.GoalKnown != nil && noTrue(v.GoalActive) && len(v.SessionIDs) > 0
	knownSessions := 0
	for id, present := range v.SessionIDs {
		if present {
			knownSessions++
			if !v.GoalKnown[id] {
				result.GoalsIdle = false
			}
		}
	}
	if knownSessions == 0 {
		result.GoalsIdle = false
	}
	result.TasksIdle = v.TaskInventoryKnown && v.Tasks != nil && noTrue(v.Tasks)
	result.UncertaintiesEmpty = v.Uncertainties != nil && noTrue(v.Uncertainties)
	result.TurnsTerminal = len(v.Turns) > 0
	for _, turn := range v.Turns {
		if turn.Active || !turn.Terminal {
			result.TurnsTerminal = false
		}
	}
	if len(v.Scopes) == 0 {
		return result, fmt.Errorf("legacy native process inventory absent")
	}
	result.ProcessScopesStopped = true
	for _, saved := range v.Scopes {
		if !saved.Complete || !saved.ControlledStop || !saved.WaitObserved {
			result.ProcessScopesStopped = false
			continue
		}
		scope, e := hostinventory.Restore(saved)
		if e != nil {
			return result, e
		}
		current := scope.Inspect()
		_ = scope.Close()
		if !current.Complete || current.RootAlive || len(current.ProcessIDs) != 0 {
			result.ProcessScopesStopped = false
		}
	}
	var integrity string
	if err = tx.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return result, fmt.Errorf("legacy native journal integrity failed")
	}
	if err = tx.Commit(); err != nil {
		return LegacyInventoryEvidence{}, err
	}
	return result, nil
}
