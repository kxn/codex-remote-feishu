package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/nativeinspection"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestPrivateOpenCodeChildHelper(t *testing.T) {
	if os.Getenv("PRIVATE_OPENCODE_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	var promptID any
	for scanner.Scan() {
		var f struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			continue
		}
		switch f.Method {
		case "initialize":
			fmt.Printf(`{"id":%q,"result":{"protocolVersion":1,"agentInfo":{"name":"OpenCode","version":"2.1.4"}}}`+"\n", f.ID)
		case "session/new":
			fmt.Printf(`{"id":%q,"result":{"sessionId":"session"}}`+"\n", f.ID)
		case "session/prompt":
			fmt.Println(`{"method":"session/update","params":{"sessionId":"session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}`)
			if os.Getenv("PRIVATE_OPENCODE_CANCEL") == "1" {
				promptID = f.ID
			} else {
				fmt.Printf(`{"id":%q,"result":{"stopReason":"end_turn"}}`+"\n", f.ID)
			}
		case "session/cancel":
			if promptID != nil {
				fmt.Printf(`{"id":%q,"result":{"stopReason":"cancelled"}}`+"\n", promptID)
			}
		}
	}
	os.Exit(0)
}
func TestPrivateOpenCodeChildInspectionUsesNativeTerminal(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancel), func(t *testing.T) { testPrivateOpenCodeChildInspection(t, cancel) })
	}
}
func testPrivateOpenCodeChildInspection(t *testing.T, cancel bool) {
	a := privateTestApp(t, agentproto.BackendOpenCode)
	child := exec.Command(os.Args[0], "-test.run=^TestPrivateOpenCodeChildHelper$")
	child.Env = append(os.Environ(), "PRIVATE_OPENCODE_CHILD=1")
	if cancel {
		child.Env = append(child.Env, "PRIVATE_OPENCODE_CANCEL=1")
	}
	child.Stderr = io.Discard
	stdin, e := child.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	stdout, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	a.prepareWorkProcess(child)
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	scope, e := a.bindWorkProcess(child)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { stdin.Close(); child.Process.Kill(); child.Wait(); scope.MarkWaitObserved() }()
	runtime := a.runtime.(*privateNativeRuntime).backendRuntime.(*opencodeBackendRuntime)
	reader, e := a.bootstrapOpenCodeACP(runtime.translator, stdin, stdout, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(reader)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "run"}
	cmd := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: a.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	if _, e = a.prepareJournalCommand(cmd); e != nil {
		t.Fatal(e)
	}
	result, e := a.runtime.TranslateCommand(cmd)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range flattenRuntimeCommandPhaseFrames(result.Phases) {
		if e = writeJournaledChildFrame(stdin, f, a.runtime); e != nil {
			t.Fatal(e)
		}
	}
	var terminal agentproto.Event
	for i := 0; i < 3; i++ {
		if cancel && i == 2 {
			activeInspect := agentproto.Command{CommandID: "active-inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &agentproto.NativeTurnBinding{Version: 1, Owner: binding.Owner}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
			active := a.inspectNative(context.Background(), activeInspect, nil)
			if active.RuntimeInspection.OwnerScope == nil || len(active.RuntimeInspection.OwnerScope.Sessions) != 1 || active.RuntimeInspection.OwnerScope.Sessions[0].StreamTerminal != nil {
				t.Fatalf("active prompt treated as stopped: %+v", active.RuntimeInspection)
			}
			stop := agentproto.Command{CommandID: "stop", Kind: agentproto.CommandTurnInterrupt, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session"}}
			if _, e = a.prepareJournalCommand(stop); e != nil {
				t.Fatal(e)
			}
			translated, e := a.runtime.TranslateCommand(stop)
			if e != nil {
				t.Fatal(e)
			}
			for _, f := range flattenRuntimeCommandPhaseFrames(translated.Phases) {
				if e = writeJournaledChildFrame(stdin, f, a.runtime); e != nil {
					t.Fatal(e)
				}
			}
		}
		if !scanner.Scan() {
			t.Fatal("child ended")
		}
		out, e := a.runtime.ObserveServer(scanner.Bytes())
		if e != nil {
			t.Fatal(e)
		}
		events, e := a.privateNative.journal.PersistEvents(out.Events)
		if e != nil {
			t.Fatal(e)
		}
		for _, ev := range events {
			if ev.Kind == agentproto.EventTurnCompleted {
				terminal = ev
			}
		}
		for _, f := range out.OutboundToChild {
			if e = writeJournaledChildFrame(stdin, f, a.runtime); e != nil {
				t.Fatal(e)
			}
		}
	}
	if terminal.TurnID == "" || terminal.NativeBinding == nil || *terminal.NativeBinding != binding {
		t.Fatalf("terminal %+v", terminal)
	}
	inspect := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, Target: agentproto.Target{ThreadID: terminal.ThreadID, TurnID: terminal.TurnID}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, SubjectCommandID: cmd.CommandID, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	event := a.inspectNative(context.Background(), inspect, nil)
	if event.RuntimeInspection.Problem != nil || event.RuntimeInspection.StreamTerminal == nil {
		t.Fatalf("inspection %+v", event.RuntimeInspection)
	}
	ownerBinding := agentproto.NativeTurnBinding{Version: 1, Owner: binding.Owner}
	ownerInspect := agentproto.Command{CommandID: "owner-inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &ownerBinding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	observed := a.inspectNative(context.Background(), ownerInspect, nil)
	if observed.RuntimeInspection.Problem != nil || observed.RuntimeInspection.OwnerScope == nil || !observed.RuntimeInspection.OwnerScope.KnownSessionsComplete || len(observed.RuntimeInspection.OwnerScope.Sessions) != 1 || observed.RuntimeInspection.OwnerScope.Sessions[0].StreamTerminal == nil {
		t.Fatalf("owner inspection %+v", observed.RuntimeInspection)
	}
	if e = nativeinspection.VerifyPrivateOwnerStopped(binding.Owner, *observed.RuntimeInspection); e != nil {
		t.Fatalf("native quiescence proof: %v background=%+v sessions=%+v", e, observed.RuntimeInspection.Background, observed.RuntimeInspection.OwnerScope.Sessions)
	}
	identity, e := hostinventory.CaptureProcess(child.Process.Pid)
	if e != nil {
		t.Fatal(e)
	}
	if e = stdin.Close(); e != nil {
		t.Fatal(e)
	}
	if e = child.Wait(); e != nil {
		t.Fatal(e)
	}
	scope.MarkWaitObserved()
	if e = hostinventory.WaitProcessExit(context.Background(), identity); e != nil {
		t.Fatal(e)
	}
	if e = a.privateNative.journal.Close(); e != nil {
		t.Fatal(e)
	}
	if e = runtimejournal.VerifyRetiredPrivateWriteFence(context.Background(), a.privateNative.snapshot, a.config.RuntimePaths.StateDir, observed.RuntimeInspection.NativeWriteWatermark); e != nil {
		t.Fatal(e)
	}
}
