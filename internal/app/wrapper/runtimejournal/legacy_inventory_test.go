//go:build linux

package runtimejournal

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestLegacyRetiredInventoryKeepsNativeDomainsSeparate(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "journal.db")
	s := openTestStore(t, path, root, cwd, "carrier")
	owner := *testCommand().Execution
	snapshot := agentproto.RuntimeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path, CarrierIncarnation: "carrier"}
	if err := s.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	hostinventory.Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := hostinventory.Bind(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	scope.MarkControlledStop()
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	scope.MarkWaitObserved()
	saved := scope.Inspect()
	_ = scope.Close()
	if !saved.Complete || saved.RootAlive || len(saved.ProcessIDs) != 0 {
		t.Fatal("fixture process did not stop")
	}
	inventory := map[string]any{"owner": owner, "scopes": []hostinventory.Snapshot{saved}, "active_native_commands": map[string]bool{}, "pending_inputs": map[string]string{}, "active_tools": map[string]bool{}, "goal_active": map[string]bool{}, "goal_known": map[string]bool{"session": true}, "session_ids": map[string]bool{"session": true}, "tasks": map[string]bool{}, "uncertainties": map[string]bool{}, "task_inventory_known": true, "turns": map[string]any{"t": map[string]bool{"Active": false, "Terminal": true}}}
	raw, _ := json.Marshal(inventory)
	if err = s.SaveInventory(raw); err != nil {
		t.Fatal(err)
	}
	receipts, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventThreadDiscovered, ThreadID: "session", Execution: &owner}})
	if err != nil || len(receipts) != 1 {
		t.Fatalf("source event fixture: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	evidence, err := VerifyLegacyRetiredInventory(t.Context(), snapshot, root)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.ProcessScopesStopped || !evidence.TurnsTerminal || !evidence.TasksIdle || !evidence.InputsIdle || !evidence.GoalsIdle {
		t.Fatal("stopped domains not proven")
	}
	different := snapshot
	different.NativeBinaryVersion = "legacy-different"
	if _, err = VerifyLegacyRetiredInventory(t.Context(), different, root); err == nil {
		t.Fatal("different launch accepted without source receipt")
	}
	correlated, err := VerifyLegacyRetiredInventoryWithReceipt(t.Context(), different, root, "session", receipts[0])
	if err != nil || correlated.SourceReceiptHash == "" {
		t.Fatalf("independently correlated journal: %v", err)
	}
	forged := receipts[0]
	forged.ThreadID = "other"
	if _, err = VerifyLegacyRetiredInventoryWithReceipt(t.Context(), different, root, "session", forged); err == nil {
		t.Fatal("foreign source receipt accepted")
	}
	inventory["turns"] = map[string]any{"t": map[string]bool{"Active": true, "Terminal": false}}
	raw, _ = json.Marshal(inventory)
	s = openTestStore(t, path, root, cwd, "carrier")
	if err = s.SaveInventory(raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	evidence, err = VerifyLegacyRetiredInventory(t.Context(), snapshot, root)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.TurnsTerminal || !evidence.ProcessScopesStopped {
		t.Fatal("active turn hidden by process exit")
	}
}
