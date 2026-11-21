package wrapper

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func newTestJournalWorkApp(t *testing.T, backend agentproto.Backend) *App {
	t.Helper()
	base := newTestWorkApp(t, backend)
	cfg := base.config
	cfg.RuntimePaths.StateDir = t.TempDir()
	cfg.WorkLaunchSnapshot.JournalPath = filepath.Join(cfg.RuntimePaths.StateDir, "worker", "runtime.db")
	cfg.WorkLaunchSnapshot.CarrierIncarnation = "carrier-test"
	app := New(cfg)
	if app.workErr != nil || app.work == nil || app.work.journal == nil {
		t.Fatalf("journal wrapper setup: %v", app.workErr)
	}
	t.Cleanup(func() { cleanupBackendRuntime(app.runtime) })
	return app
}

type journalEvidenceWriter struct {
	t         *testing.T
	app       *App
	commandID string
	buffer    bytes.Buffer
	short     bool
}

func (w *journalEvidenceWriter) Write(frame []byte) (int, error) {
	w.t.Helper()
	receipt, err := w.app.runtimeCommandReceipt(w.commandID)
	if err != nil || receipt == nil || receipt.Stage != runtimejournal.StageWriteStarted || receipt.WrittenFrames != 0 {
		w.t.Fatalf("native write preceded durable write-start or fabricated written: %#v %v", receipt, err)
	}
	if w.short {
		return len(frame) / 2, nil
	}
	return w.buffer.Write(frame)
}

func TestJournalWrapperRecordsActualWriteAndDoesNotResendDuplicate(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "written", true: "short-write-unknown"}[short], func(t *testing.T) {
			app := newTestJournalWorkApp(t, agentproto.BackendClaude)
			if _, err := app.runtime.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"native-session","permissionMode":"default"}`)); err != nil {
				t.Fatal(err)
			}
			command := testWorkCommand(app)
			command.Target.ThreadID = "native-session"
			command.Target.ExecutionMode = agentproto.PromptExecutionModeResumeExisting
			if duplicate, err := app.prepareJournalCommand(command); err != nil || duplicate {
				t.Fatalf("prepare %v %v", duplicate, err)
			}
			result, err := app.runtime.TranslateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			frames := flattenRuntimeCommandPhaseFrames(result.Phases)
			if len(frames) != 1 {
				t.Fatalf("expected one user frame, got %d", len(frames))
			}
			receipt, err := app.runtimeCommandReceipt(command.CommandID)
			if err != nil || receipt.Stage != runtimejournal.StagePrepared || receipt.WrittenFrames != 0 {
				t.Fatal("queue registration fabricated native write")
			}
			writer := &journalEvidenceWriter{t: t, app: app, commandID: command.CommandID, short: short}
			err = writeChildFrameForRuntime(writer, frames[0], app.runtime, nil, nil, nil)
			if short && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("short write=%v", err)
			}
			if !short && err != nil {
				t.Fatal(err)
			}
			receipt, err = app.runtimeCommandReceipt(command.CommandID)
			want, written := runtimejournal.StageWritten, 1
			if short {
				want, written = runtimejournal.StageUnknown, 0
			}
			if err != nil || receipt.Stage != want || receipt.WrittenFrames != written {
				t.Fatalf("wrong write evidence: %#v %v", receipt, err)
			}
			before := writer.buffer.Len()
			if duplicate, err := app.prepareJournalCommand(command); err != nil || !duplicate {
				t.Fatal("duplicate did not return existing receipt")
			}
			if writer.buffer.Len() != before {
				t.Fatal("duplicate replayed native write")
			}
			changed := command
			changed.Prompt.Inputs = append([]agentproto.Input(nil), command.Prompt.Inputs...)
			changed.Prompt.Inputs[0].Text = "different"
			if _, err := app.prepareJournalCommand(changed); err == nil {
				t.Fatal("same command ID accepted different body")
			}
		})
	}
}

func TestJournalWrapperExactRequestFollowupAndCapability(t *testing.T) {
	app := newTestJournalWorkApp(t, agentproto.BackendCodex)
	command := testWorkCommand(app)
	if _, err := app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frame := flattenRuntimeCommandPhaseFrames(result.Phases)[0]
	requestID := nativeFrameRequestID(frame)
	if err := writeChildFrameForRuntime(io.Discard, frame, app.runtime, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	observed, err := app.runtime.ObserveServer([]byte(`{"id":"` + requestID + `","result":{"thread":{"id":"native-thread"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.OutboundToChild) != 1 {
		t.Fatal("missing correlated native followup")
	}
	receipt, err := app.runtimeCommandReceipt(command.CommandID)
	if err != nil || receipt.PreparedFrames != 2 || receipt.WrittenFrames != 1 || receipt.Stage != runtimejournal.StagePartiallyWritten {
		t.Fatalf("followup frame lost original command: %#v %v", receipt, err)
	}
	if err := writeChildFrameForRuntime(io.Discard, observed.OutboundToChild[0], app.runtime, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	receipt, _ = app.runtimeCommandReceipt(command.CommandID)
	if receipt.WrittenFrames != 2 {
		t.Fatal("native followup write was not recorded")
	}
	hello := app.relayHello()
	if hello.WorkerProtocolVersion != 1 || hello.JournalProtocolVersion != 1 || hello.CarrierIncarnation != "carrier-test" {
		t.Fatal("durable capability is not separate from binding capability")
	}
	legacy := New(Config{Backend: agentproto.BackendClaude})
	if legacy.relayHello().JournalProtocolVersion != 0 {
		t.Fatal("legacy advertised journal")
	}
	bindingOnly := newTestWorkApp(t, agentproto.BackendClaude)
	if bindingOnly.relayHello().WorkerProtocolVersion != 1 || bindingOnly.relayHello().JournalProtocolVersion != 0 {
		t.Fatal("binding-only wrapper advertised durable receipt")
	}
}
