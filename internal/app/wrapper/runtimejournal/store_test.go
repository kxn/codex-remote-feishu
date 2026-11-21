package runtimejournal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func testCommand() agentproto.Command {
	return agentproto.Command{CommandID: "command-1", Kind: agentproto.CommandPromptSend, Execution: &agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1, AttemptID: "attempt", RunRef: "run"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "original input"}}}}
}

func openTestStore(t *testing.T, path, stateDir, cwd, incarnation string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path, stateDir, cwd, incarnation)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestCommandIdempotencyAndFrameEvidence(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store := openTestStore(t, filepath.Join(root, "journal.db"), root, cwd, "carrier-1")
	defer store.Close()
	command := testCommand()
	receipt, duplicate, err := store.PrepareCommand(command)
	if err != nil || duplicate || receipt.Stage != StagePrepared {
		t.Fatalf("prepare=%#v duplicate=%v err=%v", receipt, duplicate, err)
	}
	_, duplicate, err = store.PrepareCommand(command)
	if err != nil || !duplicate {
		t.Fatal("same command was not idempotent")
	}
	changed := testCommand()
	changed.Prompt.Inputs[0].Text = "different body"
	if _, _, err := store.PrepareCommand(changed); !errors.Is(err, ErrConflict) {
		t.Fatal("same ID accepted a different body")
	}
	frame := []byte("native input\n")
	first, err := store.PrepareFrame(command.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PrepareFrame(command.CommandID, frame)
	if err != nil || first == second {
		t.Fatal("equal frame bytes lost separate identities")
	}
	if err := store.StartFrame(first, []byte("wrong bytes")); !errors.Is(err, ErrConflict) {
		t.Fatal("changed frame was accepted")
	}
	if err := store.StartFrame(first, frame); err != nil {
		t.Fatal(err)
	}
	receipt, _ = store.CommandReceipt(command.CommandID)
	if receipt.Stage != StageWriteStarted || receipt.WrittenFrames != 0 {
		t.Fatal("write-start fabricated written evidence")
	}
	if err := store.FinishFrame(first, errors.New("partial write")); err != nil {
		t.Fatal(err)
	}
	if err := store.StartFrame(second, frame); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishFrame(second, nil); err != nil {
		t.Fatal(err)
	}
	receipt, _ = store.CommandReceipt(command.CommandID)
	if receipt.Stage != StageUnknown || receipt.WrittenFrames != 1 || receipt.PreparedFrames != 2 {
		t.Fatalf("later write erased earlier uncertainty: %#v", receipt)
	}
	if err := store.StartFrame(second, frame); !errors.Is(err, ErrConflict) {
		t.Fatal("written frame was allowed to replay")
	}
}

func TestJournalCrashHelper(t *testing.T) {
	mode := os.Getenv("RUNTIME_JOURNAL_CRASH_TEST")
	if mode == "" {
		return
	}
	store, err := Open(context.Background(), os.Getenv("RUNTIME_JOURNAL_TEST_PATH"), os.Getenv("RUNTIME_JOURNAL_TEST_ROOT"), os.Getenv("RUNTIME_JOURNAL_TEST_CWD"), "carrier-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PrepareCommand(testCommand()); err != nil {
		t.Fatal(err)
	}
	frame := []byte("native side effect")
	id, err := store.PrepareFrame("command-1", frame)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "prepared" {
		if err := store.StartFrame(id, frame); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "write-before-log" || mode == "written" {
		if err := os.WriteFile(os.Getenv("RUNTIME_JOURNAL_TEST_MARKER"), frame, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "written" {
		if err := store.FinishFrame(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	os.Exit(0) // deliberately leave SQLite and the carrier lock unclosed
}

func TestCrashWindowsReopenWithoutBlindReplay(t *testing.T) {
	for _, mode := range []string{"prepared", "write-started", "write-before-log", "written"} {
		t.Run(mode, func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "journal.db")
			marker := filepath.Join(root, "native-effect")
			child := exec.Command(os.Args[0], "-test.run=^TestJournalCrashHelper$")
			child.Env = append(os.Environ(), "RUNTIME_JOURNAL_CRASH_TEST="+mode, "RUNTIME_JOURNAL_TEST_PATH="+path, "RUNTIME_JOURNAL_TEST_ROOT="+root, "RUNTIME_JOURNAL_TEST_CWD="+cwd, "RUNTIME_JOURNAL_TEST_MARKER="+marker)
			if output, err := child.CombinedOutput(); err != nil {
				t.Fatalf("crash helper: %v %s", err, output)
			}
			store := openTestStore(t, path, root, cwd, "carrier-1")
			defer store.Close()
			receipt, duplicate, err := store.PrepareCommand(testCommand())
			if err != nil || !duplicate {
				t.Fatal("reopen forgot existing command")
			}
			want := StageUnknown
			if mode == "prepared" {
				want = StagePrepared
			}
			if receipt.Stage != want {
				t.Fatalf("stage=%s want %s", receipt.Stage, want)
			}
			if (receipt.WrittenFrames == 1) != (mode == "written") {
				t.Fatalf("fabricated write receipt: %#v", receipt)
			}
			_, markerErr := os.Stat(marker)
			if mode == "write-before-log" && markerErr != nil {
				t.Fatal("test did not enter native-write-before-commit window")
			}
		})
	}
}

func TestEventReplayAcknowledgementAndIncarnations(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "journal.db")
	store := openTestStore(t, path, root, cwd, "carrier-old")
	binding := testCommand().Execution
	persisted, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventItemDelta, Delta: "token"}, {Kind: agentproto.EventTurnCompleted, CommandID: "command-1", Execution: binding}})
	if err != nil || persisted[0].Journal != nil || persisted[1].Journal == nil {
		t.Fatalf("critical classification: %v", err)
	}
	old := persisted[1]
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, path, root, cwd, "carrier-new")
	defer store.Close()
	newEvents, err := store.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnStarted, CommandID: "command-2"}})
	if err != nil {
		t.Fatal(err)
	}
	page, cursor, err := store.Replay(0, 1)
	if err != nil || len(page) != 1 || page[0].Journal.CarrierIncarnation != "carrier-old" || page[0].Execution == nil || *page[0].Execution != *binding {
		t.Fatal("old incarnation or execution was rewritten")
	}
	second, _, err := store.Replay(cursor, 1)
	if err != nil || len(second) != 1 || second[0].Journal.CarrierIncarnation != "carrier-new" {
		t.Fatal("paged replay lost next incarnation")
	}
	if newEvents[0].Journal.Sequence != 1 {
		t.Fatal("new incarnation did not get its independent sequence")
	}
	receipt := agentproto.EventReceipt{CarrierIncarnation: old.Journal.CarrierIncarnation, Sequence: old.Journal.Sequence, PayloadHash: "wrong"}
	if err := store.Acknowledge(receipt); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong hash acknowledged event")
	}
	receipt.PayloadHash = old.Journal.PayloadHash
	if err := store.Acknowledge(receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.Acknowledge(receipt); err != nil {
		t.Fatal("duplicate receipt was not idempotent")
	}
	page, _, err = store.Replay(0, 10)
	if err != nil || len(page) != 1 || page[0].Journal.CarrierIncarnation != "carrier-new" {
		t.Fatal("ACK did not remove only exact event from replay")
	}
}

func TestJournalSequenceSurvivesReopenAndReplayIsBounded(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "journal.db")
	store := openTestStore(t, path, root, cwd, "same-carrier")
	events := make([]agentproto.Event, 300)
	for i := range events {
		events[i].Kind = agentproto.EventTurnStarted
	}
	if _, err := store.PersistEvents(events); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openTestStore(t, path, root, cwd, "same-carrier")
	defer store.Close()
	result, err := store.PersistEvents(events[:1])
	if err != nil || result[0].Journal.Sequence != 301 {
		t.Fatal("process restart reset event sequence")
	}
	var cursor int64
	count := 0
	for {
		page, next, err := store.Replay(cursor, 128)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 128 || next <= cursor {
			t.Fatal("unbounded or stuck replay")
		}
		count += len(page)
		cursor = next
	}
	if count != 301 {
		t.Fatalf("replay count=%d", count)
	}
}

func TestJournalRejectsWorkspaceAndEscapingPaths(t *testing.T) {
	root, cwd, outside := t.TempDir(), t.TempDir(), t.TempDir()
	for _, path := range []string{filepath.Join(cwd, "journal.db"), filepath.Join(outside, "journal.db"), "relative.db", root} {
		if _, err := ValidatePath(path, root, cwd); err == nil {
			t.Fatalf("accepted uncontrolled journal path %q", path)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidatePath(filepath.Join(root, "escape", "journal.db"), root, cwd); err == nil {
			t.Fatal("symlink parent escaped controlled state root")
		}
	}
	path := filepath.Join(root, "controlled", "journal.db")
	store := openTestStore(t, path, root, cwd, "carrier")
	defer store.Close()
	if other, err := Open(context.Background(), path, root, cwd, "carrier"); err == nil {
		other.Close()
		t.Fatal("two live wrappers acquired one journal")
	}
}
