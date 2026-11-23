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

func TestPrivateContractCannotRebindTeamIncarnation(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store := openTestStore(t, filepath.Join(root, "runtime.db"), root, cwd, "inc")
	defer store.Close()
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "runtime.db")}
	if err := store.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.BindLaunchSnapshot(agentproto.RuntimeLaunchSnapshot{Version: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("team rebind: %v", err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "execution"}
	command := agentproto.Command{CommandID: "private", Kind: agentproto.CommandPromptSend, NativeBinding: &binding}
	if _, duplicate, err := store.PreparePrivateCommand(command); err != nil || duplicate {
		t.Fatalf("prepare: %v %v", duplicate, err)
	}
	if _, duplicate, err := store.PreparePrivateCommand(command); err != nil || !duplicate {
		t.Fatalf("replay: %v %v", duplicate, err)
	}
	if _, _, err := store.PrepareCommand(command); !errors.Is(err, ErrConflict) {
		t.Fatalf("team accepted private command: %v", err)
	}
}

func TestPrivateJournalReceiptReplayAfterReopen(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "journal.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	s := openTestStore(t, path, root, cwd, "inc")
	if err := s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
	command := agentproto.Command{CommandID: "command", Kind: agentproto.CommandTurnSteer, NativeBinding: &binding}
	if _, _, err := s.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	events, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: "command", NativeBinding: &binding, RequestID: "rpc", ThreadID: "session", TurnID: "turn", Status: "accepted"}})
	if err != nil || len(events) != 1 || events[0].Journal == nil {
		t.Fatalf("persist: %v", err)
	}
	original := events[0]
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, path, root, cwd, "inc")
	defer s.Close()
	if err = s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	events, _, err = s.Replay(0, 16)
	if err != nil || len(events) != 1 || events[0].Journal.PayloadHash != original.Journal.PayloadHash || events[0].NativeBinding == nil || *events[0].NativeBinding != binding {
		t.Fatalf("replay: %+v %v", events, err)
	}
	receipt := agentproto.EventReceipt{CarrierIncarnation: "inc", Sequence: original.Journal.Sequence, PayloadHash: original.Journal.PayloadHash}
	if err = s.Acknowledge(receipt); err != nil {
		t.Fatal(err)
	}
	events, _, err = s.Replay(0, 16)
	if err != nil || len(events) != 0 {
		t.Fatalf("acked replay: %+v %v", events, err)
	}
}

func TestReadPrivateReceiptAfterLostAckAndUncertainWrite(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "runtime.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	s := openTestStore(t, path, root, cwd, "inc")
	defer s.Close()
	if err := s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
	command := agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, NativeBinding: &binding}
	if _, _, err := s.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"rpc","method":"turn/start"}`)
	id, err := s.PrepareFrame(command.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishFrame(id, errors.New("write interrupted")); err != nil {
		t.Fatal(err)
	}
	receipt, err := ReadPrivateCommandReceipt(context.Background(), snapshot, root, command.CommandID)
	if err != nil || len(receipt.Frames) != 1 || receipt.Frames[0].NativeRequestID != "rpc" || receipt.Frames[0].Stage != StageUnknown {
		t.Fatalf("unknown receipt: %+v %v", receipt, err)
	}
}

func TestRetiredPrivateFenceChecksFrozenContractAndLaterWrites(t *testing.T) {
	ctx := context.Background()
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "runtime.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
	s := openTestStore(t, path, root, cwd, "inc")
	if err := s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRetiredPrivateWriteFence(ctx, snapshot, root, 0); err == nil {
		t.Fatal("live writer journal was treated as retired")
	}
	cmd := agentproto.Command{CommandID: "first", Kind: agentproto.CommandPromptSend, NativeBinding: &binding}
	if _, _, err := s.PreparePrivateCommand(cmd); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"rpc1","method":"turn/start"}`)
	id, err := s.PrepareFrame(cmd.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishFrame(id, nil); err != nil {
		t.Fatal(err)
	}
	watermark, err := s.NativeWriteWatermark()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = VerifyRetiredPrivateWriteFence(ctx, snapshot, root, watermark); err != nil {
		t.Fatal(err)
	}
	wrong := snapshot
	wrong.Owner.Epoch++
	if err = VerifyRetiredPrivateWriteFence(ctx, wrong, root, watermark); err == nil {
		t.Fatal("changed launch accepted")
	}
	s = openTestStore(t, path, root, cwd, "inc")
	if err = s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	cmd.CommandID = "later"
	if _, _, err = s.PreparePrivateCommand(cmd); err != nil {
		t.Fatal(err)
	}
	frame = []byte(`{"id":"rpc2","method":"turn/steer"}`)
	id, err = s.PrepareFrame(cmd.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishFrame(id, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if err = VerifyRetiredPrivateWriteFence(ctx, snapshot, root, watermark); err == nil {
		t.Fatal("late native write ignored")
	}
}
func TestRetiredPrivateFenceSupportsVerifiedEmptyJournal(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "runtime.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	s := openTestStore(t, path, root, cwd, "inc")
	if err := s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRetiredPrivateWriteFence(context.Background(), snapshot, root, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredTeamZeroWatermarkRequiresRealMatchingJournal(t *testing.T) {
	ctx := context.Background()
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "runtime.db")
	snapshot := agentproto.RuntimeLaunchSnapshot{Version: 1, CWD: cwd, JournalPath: path, CarrierIncarnation: "inc"}
	if err := VerifyRetiredWriteFence(ctx, snapshot, root, 0); err == nil {
		t.Fatal("missing journal proved zero writes")
	}
	store := openTestStore(t, path, root, cwd, "inc")
	if err := store.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRetiredWriteFence(ctx, snapshot, root, 0); err != nil {
		t.Fatalf("verified empty journal rejected: %v", err)
	}
	store = openTestStore(t, path, root, cwd, "inc")
	cmd := testCommand()
	if _, _, err := store.PrepareCommand(cmd); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"rpc","method":"turn/start"}`)
	if _, err := store.PrepareFrame(cmd.CommandID, frame); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRetiredWriteFence(ctx, snapshot, root, 0); err == nil {
		t.Fatal("later effectful frame accepted as zero writes")
	}
}
