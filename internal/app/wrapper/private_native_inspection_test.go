package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateNativeInspectionRealChild(t *testing.T) {
	for _, tc := range []struct {
		name               string
		active, background bool
	}{{"stopped", false, false}, {"active_goal", true, false}, {"background", false, true}} {
		t.Run(tc.name, func(t *testing.T) { testPrivateNativeInspectionRealChild(t, tc.active, tc.background) })
	}
}
func testPrivateNativeInspectionRealChild(t *testing.T, active, background bool) {
	app := privateTestApp(t, agentproto.BackendCodex)
	owner := app.privateNative.snapshot.Owner
	child := exec.Command(os.Args[0], "-test.run=^TestWorkInspectionNativeChildHelper$")
	child.Env = append(os.Environ(), "WORK_INSPECTION_CHILD=1")
	if active {
		child.Env = append(child.Env, "WORK_INSPECTION_ACTIVE_GOAL=1")
	}
	if background {
		child.Env = append(child.Env, "WORK_INSPECTION_START_BACKGROUND=1")
	}
	child.Stderr = io.Discard
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	app.prepareWorkProcess(child)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := app.bindWorkProcess(child)
	if err != nil {
		child.Process.Kill()
		t.Fatal(err)
	}
	defer func() { stdin.Close(); child.Process.Kill(); child.Wait(); scope.MarkWaitObserved() }()
	reader, err := app.bootstrapHeadlessCodex(stdin, stdout, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan []byte, 16)
	problems := make(chan error, 4)
	terminal := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-writes:
				if e := writeJournaledChildFrame(stdin, frame, app.runtime); e != nil {
					problems <- e
					return
				}
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			result, e := app.runtime.ObserveServer(scanner.Bytes())
			if e != nil {
				problems <- e
				return
			}
			if len(result.Events) > 0 {
				persisted, e := app.privateNative.journal.PersistEvents(result.Events)
				if e != nil {
					problems <- e
					return
				}
				for _, event := range persisted {
					if event.Kind == agentproto.EventTurnCompleted {
						select {
						case terminal <- struct{}{}:
						default:
						}
					}
				}
			}
			for _, frame := range result.OutboundToChild {
				select {
				case writes <- frame:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
	prompt := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	if _, err = app.prepareJournalCommand(prompt); err != nil {
		t.Fatal(err)
	}
	translated, err := app.runtime.TranslateCommand(prompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range flattenRuntimeCommandPhaseFrames(translated.Phases) {
		writes <- frame
	}
	select {
	case <-terminal:
	case e := <-problems:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("child did not finish turn")
	}
	rawInventory, e := app.privateNative.journal.LoadInventory()
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(rawInventory), "workerSessionId") || strings.Contains(string(rawInventory), "scopeId") {
		t.Fatalf("private inventory contains team binding: %s", rawInventory)
	}
	inspectOwner := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	command := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &inspectOwner, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	if _, err = app.prepareWorkCommand(command); err != nil {
		t.Fatal(err)
	}
	if _, err = app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	event := app.inspectNative(ctx, command, writes)
	if event.RuntimeInspection == nil || event.RuntimeInspection.Problem != nil {
		raw, _ := json.Marshal(event)
		t.Fatalf("inspection: %s", raw)
	}
	value := event.RuntimeInspection
	if value.CarrierIncarnation != "inc" || value.LocationGeneration != 0 || value.NativeWriteWatermark <= 0 || value.OwnerScope == nil || len(value.OwnerScope.Sessions) != 1 || value.OwnerScope.Sessions[0].SessionID != "session" {
		t.Fatalf("bad owner inventory: %+v", value)
	}
	if !value.Background.Complete || !value.Background.NativeInputsComplete || !value.Background.NativeTaskSetComplete || !value.Background.ContinuationInventoryKnown || len(value.Background.ActiveNativeTurnIDs) != 0 {
		t.Fatalf("incomplete inventory: %+v", value.Background)
	}
	if active && len(value.Background.ActiveGoalSessionIDs) != 1 {
		t.Fatalf("active goal missing: %+v", value.Background)
	}
	if !active && len(value.Background.ActiveGoalSessionIDs) != 0 {
		t.Fatalf("unexpected goal: %+v", value.Background)
	}
	if background {
		if len(value.Background.RunningProcessIDs) == 0 && len(value.Background.PendingTaskIDs) == 0 {
			t.Fatal("live background missed")
		}
		stop := command
		stop.CommandID = "inspect-stop"
		request := *command.RuntimeInspection
		request.StopBackground = true
		stop.RuntimeInspection = &request
		if _, err = app.prepareJournalCommand(stop); err != nil {
			t.Fatal(err)
		}
		stopped := app.inspectNative(ctx, stop, writes)
		if stopped.RuntimeInspection.Problem != nil || len(stopped.RuntimeInspection.Background.RunningProcessIDs) != 0 || len(stopped.RuntimeInspection.Background.PendingTaskIDs) != 0 {
			t.Fatalf("background stop unproven: %+v", stopped.RuntimeInspection)
		}
	}
	wrong := command
	request := *command.RuntimeInspection
	request.ExpectedProcessGeneration++
	wrong.RuntimeInspection = &request
	if bad := app.inspectNative(ctx, wrong, writes); bad.RuntimeInspection.Problem == nil {
		t.Fatal("wrong generation accepted")
	}
}

func TestPrivateExactCarrierExitRequiresRealProcessExit(t *testing.T) {
	child := exec.Command(os.Args[0], "-test.run=^TestWorkInspectionNativeChildHelper$")
	child.Env = append(os.Environ(), "WORK_INSPECTION_CHILD=1", "WORK_INSPECTION_BACKGROUND_WAIT=1")
	child.Stdout = io.Discard
	child.Stderr = io.Discard
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { child.Process.Kill(); child.Wait() }()
	identity, err := hostinventory.CaptureProcess(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err = hostinventory.WaitProcessExit(ctx, identity); err == nil {
		t.Fatal("live process falsely verified exited")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err == nil {
		t.Fatal("expected killed child")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err = hostinventory.WaitProcessExit(ctx2, identity); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateClaudeInspectionChild(t *testing.T) {
	if os.Getenv("PRIVATE_CLAUDE_INSPECTION_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var frame map[string]any
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(2)
		}
		switch frame["type"] {
		case "control_request":
			id, _ := frame["request_id"].(string)
			fmt.Println(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)
			fmt.Printf(`{"type":"control_response","response":{"request_id":%q,"subtype":"success","response":{}}}`+"\n", id)
		case "user":
			uuid, _ := frame["uuid"].(string)
			fmt.Printf(`{"type":"user","uuid":%q,"session_id":"session","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`+"\n", uuid)
			fmt.Println(`{"type":"assistant","session_id":"session","message":{"id":"reply","content":[{"type":"text","text":"done"}]}}`)
			fmt.Println(`{"type":"result","subtype":"success","session_id":"session","result":"done","is_error":false}`)
		}
	}
	os.Exit(0)
}
func TestPrivateClaudeInspectionNativeUUIDTerminal(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendClaude)
	owner := app.privateNative.snapshot.Owner
	child := exec.Command(os.Args[0], "-test.run=^TestPrivateClaudeInspectionChild$")
	child.Env = append(os.Environ(), "PRIVATE_CLAUDE_INSPECTION_CHILD=1")
	child.Stderr = io.Discard
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	app.prepareWorkProcess(child)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := app.bindWorkProcess(child)
	if err != nil {
		child.Process.Kill()
		t.Fatal(err)
	}
	defer func() { stdin.Close(); child.Process.Kill(); child.Wait(); scope.MarkWaitObserved() }()
	reader, err := app.bootstrapClaude(stdin, stdout, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := make(chan []byte, 8)
	terminal := make(chan agentproto.Event, 1)
	problems := make(chan error, 4)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-writes:
				if e := writeJournaledChildFrame(stdin, frame, app.runtime); e != nil {
					problems <- e
					return
				}
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			result, e := app.runtime.ObserveServer(scanner.Bytes())
			if e != nil {
				problems <- e
				return
			}
			events, e := app.privateNative.journal.PersistEvents(result.Events)
			if e != nil {
				problems <- e
				return
			}
			for _, event := range events {
				if event.Kind == agentproto.EventTurnCompleted {
					select {
					case terminal <- event:
					default:
					}
				}
			}
			for _, frame := range result.OutboundToChild {
				select {
				case writes <- frame:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "run"}
	prompt := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ThreadID: "session", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hi"}}}}
	if _, err = app.prepareJournalCommand(prompt); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(prompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range flattenRuntimeCommandPhaseFrames(result.Phases) {
		writes <- frame
	}
	var end agentproto.Event
	select {
	case end = <-terminal:
	case e := <-problems:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("Claude child did not finish")
	}
	if end.NativeBinding == nil || *end.NativeBinding != binding || end.ThreadID != "session" || end.TurnID == "" {
		t.Fatalf("terminal lacks native identity: %+v", end)
	}
	inspect := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, Target: agentproto.Target{ThreadID: end.ThreadID, TurnID: end.TurnID}, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, SubjectCommandID: prompt.CommandID, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	if _, err = app.prepareJournalCommand(inspect); err != nil {
		t.Fatal(err)
	}
	event := app.inspectNative(ctx, inspect, writes)
	if event.RuntimeInspection.Problem != nil || event.RuntimeInspection.StreamTerminal == nil {
		t.Fatalf("inspection: %+v", event.RuntimeInspection)
	}
	if event.RuntimeInspection.StreamTerminal.NativeBinding == nil || *event.RuntimeInspection.StreamTerminal.NativeBinding != binding {
		t.Fatal("terminal owner lost")
	}
}

func TestPrivateInspectionFailsClosedWithoutIdentityEvidence(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	owner := app.privateNative.snapshot.Owner
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner}
	command := agentproto.Command{CommandID: "inspect", Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1}}
	if event := app.inspectNative(context.Background(), command, nil); event.RuntimeInspection.Problem == nil || event.RuntimeInspection.Problem.Code != "native_initialization_unproven" {
		t.Fatalf("missing bootstrap accepted: %+v", event.RuntimeInspection)
	}
	binding.ExecutionID = "run"
	command.Target = agentproto.Target{ThreadID: "session", TurnID: "turn"}
	command.RuntimeInspection.SubjectCommandID = "missing"
	if event := app.inspectNative(context.Background(), command, nil); event.RuntimeInspection.Problem == nil || event.RuntimeInspection.Problem.Code != "native_inspection_target_unproven" {
		t.Fatalf("missing target proof accepted: %+v", event.RuntimeInspection)
	}
	binding.ExecutionID = ""
	command.Target = agentproto.Target{ThreadID: "session", TurnID: "turn"}
	command.RuntimeInspection.SubjectCommandID = ""
	if event := app.inspectNative(context.Background(), command, nil); event.RuntimeInspection.Problem == nil {
		t.Fatal("owner-wide inspection accepted invented target")
	}
}
