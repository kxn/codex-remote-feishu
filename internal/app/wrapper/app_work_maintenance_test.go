package wrapper

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func maintenanceFixture(t *testing.T) (*App, agentproto.Command) {
	t.Helper()
	app := newTestJournalWorkApp(t, agentproto.BackendCodex)
	owner := app.work.snapshot.Owner
	inspect := agentproto.Command{CommandID: "preflight", Kind: agentproto.CommandRuntimeInspect, Execution: &owner}
	if _, err := app.prepareJournalCommand(inspect); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"read","method":"thread/read","params":{"threadId":"session"}}`)
	id, err := app.work.journal.PrepareFrame(inspect.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.work.journal.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = app.work.journal.FinishFrame(id, nil); err != nil {
		t.Fatal(err)
	}
	result := &agentproto.RuntimeInspectionResult{NativeWriteWatermark: id, Background: agentproto.BackgroundInspection{Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true}}
	_, err = app.work.journal.PersistEvents([]agentproto.Event{{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: inspect.CommandID, Execution: &owner, RuntimeInspection: result}})
	if err != nil {
		t.Fatal(err)
	}
	cmd := agentproto.Command{CommandID: "compact", Kind: agentproto.CommandThreadCompactStart, Execution: &owner, Target: agentproto.Target{ThreadID: "session", CWD: app.work.snapshot.CWD}, Maintenance: &agentproto.NativeMaintenanceCommand{Version: 1, PreflightInspectionCommandID: inspect.CommandID, NativeWriteWatermark: id}}
	return app, cmd
}

func TestMaintenanceRequiresActualPreflightAndBuffersTerminalUntilRPCSuccess(t *testing.T) {
	app, cmd := maintenanceFixture(t)
	if _, err := app.prepareJournalCommand(cmd); err != nil {
		t.Fatal(err)
	}
	translated, err := app.runtime.TranslateCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(translated.Phases)
	if len(frames) != 1 {
		t.Fatalf("frames=%d", len(frames))
	}
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	var rpc map[string]any
	if err = json.Unmarshal(frames[0], &rpc); err != nil {
		t.Fatal(err)
	}
	// The target session is restored before its compact frame is emitted.
	if rpc["method"] == "thread/resume" {
		response, _ := json.Marshal(map[string]any{"id": rpc["id"], "result": map[string]any{"thread": map[string]any{"id": "session"}}})
		observed, e := app.runtime.ObserveServer(response)
		if e != nil {
			t.Fatal(e)
		}
		if len(observed.OutboundToChild) != 1 {
			t.Fatalf("compact followup=%d", len(observed.OutboundToChild))
		}
		if e = writeJournaledChildFrame(io.Discard, observed.OutboundToChild[0], app.runtime); e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(observed.OutboundToChild[0], &rpc); e != nil {
			t.Fatal(e)
		}
	}
	if rpc["method"] != "thread/compact/start" {
		t.Fatalf("method=%v", rpc["method"])
	}
	for _, raw := range []string{
		`{"method":"turn/started","params":{"threadId":"session","turn":{"id":"maintenance-turn"}}}`,
		`{"method":"item/completed","params":{"threadId":"session","turnId":"maintenance-turn","item":{"id":"compaction-item","type":"contextCompaction","status":"completed"}}}`,
	} {
		observed, e := app.runtime.ObserveServer([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		for _, event := range observed.Events {
			if event.Kind == agentproto.EventNativeMaintenanceTerminal || event.Kind == agentproto.EventTurnStarted {
				t.Fatal("unaccepted maintenance leaked into business lifecycle")
			}
		}
	}
	response, _ := json.Marshal(map[string]any{"id": rpc["id"], "result": map[string]any{}})
	observed, err := app.runtime.ObserveServer(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Events) != 2 || observed.Events[0].Kind != agentproto.EventNativeMaintenanceStarted || observed.Events[1].Kind != agentproto.EventNativeMaintenanceTerminal {
		t.Fatalf("maintenance results=%#v", observed.Events)
	}
	for _, event := range observed.Events {
		if event.CommandID != cmd.CommandID || event.RequestID != rpc["id"] || event.Execution == nil || *event.Execution != *cmd.Execution {
			t.Fatal("maintenance lost exact command/owner identity")
		}
	}
	if _, err = app.work.journal.PersistEvents(observed.Events); err != nil {
		t.Fatal(err)
	}
	if _, err = app.prepareWorkCommand(testWorkCommand(app)); err == nil {
		t.Fatal("item completion alone reopened input before native turn terminal")
	}
}

func TestMaintenanceRejectsChangedNativeWatermark(t *testing.T) {
	app, cmd := maintenanceFixture(t)
	other := testWorkCommand(app)
	if _, err := app.prepareJournalCommand(other); err != nil {
		t.Fatal(err)
	}
	if _, err := app.work.journal.PrepareFrame(other.CommandID, []byte(`{"id":"late","method":"turn/start"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareWorkCommand(cmd); err == nil {
		t.Fatal("late native input bypassed preflight boundary")
	}
}
