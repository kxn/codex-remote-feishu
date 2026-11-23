package wrapper

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func privateTestApp(t *testing.T, backend agentproto.Backend) *App {
	t.Helper()
	cwd, root := t.TempDir(), t.TempDir()
	mode := "app-server"
	if backend == agentproto.BackendClaude {
		mode = "claude-app-server"
	} else if backend == agentproto.BackendOpenCode {
		mode = "opencode-acp"
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "opaque", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
	app := New(Config{Backend: backend, InstanceID: "instance", Managed: true, Source: "headless", Args: []string{mode}, WorkspaceRoot: cwd, RuntimePaths: relayruntime.Paths{StateDir: root}, PrivateNativeLaunch: &agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "private", "runtime.db")}})
	if app.workErr != nil || app.privateNative == nil {
		t.Fatalf("private launch: %v", app.workErr)
	}
	t.Cleanup(func() { cleanupBackendRuntime(app.runtime) })
	return app
}
func TestPrivateLaunchOwnerAndCatalog(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner}
	for _, kind := range []agentproto.CommandKind{agentproto.CommandModelList, agentproto.CommandThreadsRefresh, agentproto.CommandThreadHistoryRead, agentproto.CommandThreadRead, agentproto.CommandThreadCompactStart, agentproto.CommandProcessExit, agentproto.CommandMCPOAuthLogin} {
		if _, err := app.prepareWorkCommand(agentproto.Command{CommandID: string(kind), Kind: kind, NativeBinding: &binding}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	other := binding
	other.Owner.Epoch++
	if _, err := app.prepareWorkCommand(agentproto.Command{CommandID: "bad", Kind: agentproto.CommandModelList, NativeBinding: &other}); err == nil {
		t.Fatal("cross-owner accepted")
	}
	mixed := agentproto.Command{CommandID: "mixed", Kind: agentproto.CommandModelList, NativeBinding: &binding, Execution: &agentproto.ExecutionBinding{}}
	if _, err := app.prepareWorkCommand(mixed); err == nil {
		t.Fatal("mixed binding accepted")
	}
	hello := app.relayHello()
	if hello.PrivateNativeProtocolVersion != 1 || hello.JournalProtocolVersion != 1 || hello.WorkerProtocolVersion != 0 || hello.CarrierIncarnation != "inc" {
		t.Fatalf("hello: %+v", hello)
	}
}
func TestPrivateCodexProtocolJournalStartAndSteer(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "turn-owner"}
	command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	if _, err := app.prepareWorkCommand(command); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) == 0 {
		t.Fatal("missing start frame")
	}
	id := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"session"}}}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.OutboundToChild) != 1 {
		t.Fatal("missing correlated turn/start")
	}
	frame := observed.OutboundToChild[0]
	request := nativeFrameRequestID(frame)
	if err = writeJournaledChildFrame(io.Discard, frame, app.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err = app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"turn"}}}`, request)))
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Events) != 2 || observed.Events[0].Kind != agentproto.EventNativeTurnBindingObserved || observed.Events[0].NativeBinding == nil || observed.Events[0].RequestID != request || observed.Events[1].Kind != agentproto.EventNativeInputReceipt || observed.Events[1].RequestID != request || observed.Events[1].NativeBinding == nil || *observed.Events[0].NativeBinding != binding || *observed.Events[1].NativeBinding != binding || observed.Events[1].Status != "accepted" {
		t.Fatalf("start evidence %+v", observed.Events)
	}
	if _, err = app.privateNative.journal.PersistEvents(observed.Events); err != nil {
		t.Fatal(err)
	}
	delta, err := app.runtime.ObserveServer([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"session","turnId":"turn","itemId":"item","delta":"hello"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Events) != 1 || delta.Events[0].Kind != agentproto.EventItemDelta || delta.Events[0].NativeBinding == nil || *delta.Events[0].NativeBinding != binding {
		t.Fatalf("live binding not proved: %+v", delta.Events)
	}
	live, err := app.privateNative.journal.PersistEvents(delta.Events)
	if err != nil || len(live) != 1 || live[0].Live == nil {
		t.Fatalf("live sequence: %+v %v", live, err)
	}
	steer := agentproto.Command{CommandID: "steer", Kind: agentproto.CommandTurnSteer, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "update"}}}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "input", InputIDs: []string{"input"}}}
	if _, err = app.prepareWorkCommand(steer); err != nil {
		t.Fatal(err)
	}
	if _, err = app.prepareJournalCommand(steer); err != nil {
		t.Fatal(err)
	}
	result, err = app.runtime.TranslateCommand(steer)
	if err != nil {
		t.Fatal(err)
	}
	frames = flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatalf("steer frames %d", len(frames))
	}
	id = nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err = app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turnId":"turn"}}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Events) != 2 || observed.Events[0].Kind != agentproto.EventNativeTurnInputAccepted || observed.Events[0].NativeBinding == nil || *observed.Events[0].NativeBinding != binding || observed.Events[0].RequestID != id || observed.Events[1].Kind != agentproto.EventNativeInputReceipt || observed.Events[1].NativeBinding == nil || *observed.Events[1].NativeBinding != binding || observed.Events[1].RequestID != id || observed.Events[1].Status != "accepted" {
		t.Fatalf("steer evidence %+v", observed.Events)
	}
	persisted, err := app.privateNative.journal.PersistEvents(observed.Events)
	if err != nil || persisted[0].Journal == nil {
		t.Fatalf("journal: %v", err)
	}
	if _, err = json.Marshal(persisted[0]); err != nil {
		t.Fatal(err)
	}
	replay, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turnId":"turn"}}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range replay.Events {
		if event.Kind == agentproto.EventNativeTurnInputAccepted || event.Kind == agentproto.EventNativeInputReceipt {
			t.Fatalf("duplicate receipt: %+v", event)
		}
	}
}

func TestPrivateClaudeProtocolJournalStartAndSteer(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendClaude)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "turn-owner"}
	if _, err := app.runtime.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	start := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ThreadID: "session", ExecutionMode: agentproto.PromptExecutionModeResumeExisting}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	if _, err := app.prepareJournalCommand(start); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(start)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatalf("frames %d", len(frames))
	}
	var payload struct {
		UUID string `json:"uuid"`
	}
	if err = json.Unmarshal(frames[0], &payload); err != nil || payload.UUID == "" {
		t.Fatalf("uuid: %v", err)
	}
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	echo := fmt.Sprintf(`{"type":"user","uuid":%q,"session_id":"session","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`, payload.UUID)
	if _, err = app.runtime.ObserveServer([]byte(echo)); err != nil {
		t.Fatal(err)
	}
	observed, err := app.runtime.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"message","content":[{"type":"text","text":"working"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var turn string
	for _, event := range observed.Events {
		if event.Kind == agentproto.EventTurnStarted {
			turn = event.TurnID
			if event.NativeBinding == nil || event.RequestID != payload.UUID {
				t.Fatalf("start binding %+v", event)
			}
		}
	}
	if turn == "" {
		t.Fatal("missing native start")
	}
	if _, err = app.privateNative.journal.PersistEvents(observed.Events); err != nil {
		t.Fatal(err)
	}
	steer := agentproto.Command{CommandID: "steer", Kind: agentproto.CommandTurnSteer, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: turn}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "change"}}}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "input", InputIDs: []string{"input"}}}
	if _, err = app.prepareWorkCommand(steer); err != nil {
		t.Fatal(err)
	}
	if _, err = app.prepareJournalCommand(steer); err != nil {
		t.Fatal(err)
	}
	result, err = app.runtime.TranslateCommand(steer)
	if err != nil {
		t.Fatal(err)
	}
	frames = flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatal("missing steer frame")
	}
	if err = json.Unmarshal(frames[0], &payload); err != nil {
		t.Fatal(err)
	}
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	echo = fmt.Sprintf(`{"type":"user","uuid":%q,"session_id":"session","message":{"role":"user","content":[{"type":"text","text":"change"}]}}`, payload.UUID)
	observed, err = app.runtime.ObserveServer([]byte(echo))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range observed.Events {
		if event.Kind == agentproto.EventNativeTurnInputAccepted {
			found = event.NativeBinding != nil && event.RequestID == payload.UUID && event.TurnID == turn
		}
	}
	if !found {
		t.Fatalf("missing exact native acceptance %+v", observed.Events)
	}
}

func TestPrivateNativeFixtureProcess(t *testing.T) {
	if os.Getenv("PRIVATE_NATIVE_TEST_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for scanner.Scan() {
		var frame struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		var body string
		switch frame.Method {
		case "thread/start":
			body = `{"thread":{"id":"session"}}`
		case "turn/start":
			body = `{"turn":{"id":"turn"}}`
		default:
			body = `{}`
		}
		fmt.Fprintf(writer, "{\"id\":%q,\"result\":%s}\n", frame.ID, body)
		writer.Flush()
	}
	os.Exit(0)
}
func TestPrivateNativeControlledChildReceipts(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "execution"}
	command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	if _, err := app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestPrivateNativeFixtureProcess$")
	child.Env = append(os.Environ(), "PRIVATE_NATIVE_TEST_CHILD=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); child.Wait() }()
	scanner := bufio.NewScanner(output)
	frame := flattenRuntimeCommandPhaseFrames(result.Phases)[0]
	if err = writeJournaledChildFrame(input, frame, app.runtime); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() {
		t.Fatal("missing child response")
	}
	observed, err := app.runtime.ObserveServer(scanner.Bytes())
	if err != nil || len(observed.OutboundToChild) != 1 {
		t.Fatalf("thread response: %+v %v", observed, err)
	}
	if err = writeJournaledChildFrame(input, observed.OutboundToChild[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() {
		t.Fatal("missing turn response")
	}
	observed, err = app.runtime.ObserveServer(scanner.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.Events) != 2 || observed.Events[0].Kind != agentproto.EventNativeTurnBindingObserved || observed.Events[0].NativeBinding == nil || observed.Events[1].Kind != agentproto.EventNativeInputReceipt || observed.Events[1].NativeBinding == nil {
		t.Fatalf("missing child proof: %+v", observed.Events)
	}
	if _, err = app.privateNative.journal.PersistEvents(observed.Events); err != nil {
		t.Fatal(err)
	}
	receipt, err := app.runtimeCommandReceipt(command.CommandID)
	if err != nil || receipt.WrittenFrames != 2 {
		t.Fatalf("write receipt: %+v %v", receipt, err)
	}
}

func TestPrivateLaunchRejectsMixedModesAndBackend(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	cfg := app.config
	cfg.WorkLaunchSnapshot = &agentproto.RuntimeLaunchSnapshot{Version: 1}
	mixed := New(cfg)
	if mixed.workErr == nil {
		t.Fatal("mixed launch accepted")
	}
	cfg.WorkLaunchSnapshot = nil
	cfg.Args = []string{"claude-app-server"}
	wrongMode := New(cfg)
	if wrongMode.workErr == nil {
		t.Fatal("wrong launch mode accepted")
	}
	cfg.Args = []string{"app-server"}
	cfg.Backend = agentproto.BackendClaude
	wrongBackend := New(cfg)
	if wrongBackend.workErr == nil {
		t.Fatal("wrong backend accepted")
	}
}
func TestPrivateSteerRejectsUnprovenTurn(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "run"}
	command := agentproto.Command{CommandID: "steer", Kind: agentproto.CommandTurnSteer, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: 1, ID: "input", InputIDs: []string{"input"}}}
	if _, err := app.prepareWorkCommand(command); err == nil {
		t.Fatal("steer without journaled turn accepted")
	}
}

func TestPrivateThreadRefreshSharesRegisteredSourceWithoutDuplicateSnapshot(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner}
	first := agentproto.Command{CommandID: "query-one", Kind: agentproto.CommandThreadsRefresh, NativeBinding: &binding}
	second := first
	second.CommandID = "query-two"
	if _, err := app.prepareJournalCommand(first); err != nil {
		t.Fatal(err)
	}
	a, err := app.runtime.TranslateCommand(first)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(a.Phases)
	if len(frames) != 1 {
		t.Fatalf("source frames=%d", len(frames))
	}
	request := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	if _, err = app.prepareJournalCommand(second); err != nil {
		t.Fatal(err)
	}
	b, err := app.runtime.TranslateCommand(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(flattenRuntimeCommandPhaseFrames(b.Phases)) != 0 {
		t.Fatal("waiter wrote duplicate query")
	}
	link, err := runtimejournal.ReadPrivateQueryLink(t.Context(), app.privateNative.snapshot, app.config.RuntimePaths.StateDir, second.CommandID)
	if err != nil || link.SourceCommandID != first.CommandID || link.RequestID != request || len(link.SourceReceipt.Frames) != 1 {
		t.Fatalf("link=%+v err=%v", link, err)
	}
	observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"data":[]}}`, request)))
	if err != nil {
		t.Fatal(err)
	}
	snapshots, completions := 0, 0
	for _, event := range observed.Events {
		switch event.Kind {
		case agentproto.EventThreadsSnapshot:
			snapshots++
		case agentproto.EventNativeQueryCompleted:
			completions++
			if event.NativeBinding == nil || event.RequestID != request || event.Metadata["sourceCommandId"] != first.CommandID || event.Metadata["sourceCommandHash"] != link.SourceCommandHash {
				t.Fatalf("completion: %+v", event)
			}
		}
	}
	if snapshots != 1 || completions != 2 {
		t.Fatalf("snapshots=%d completions=%d events=%+v", snapshots, completions, observed.Events)
	}
}

func TestPrivateRefreshAfterRejectedParentQueryStillCompletes(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	if _, err := app.runtime.ObserveClient([]byte(`{"id":"helper","method":"thread/list","params":{"limit":50}}`)); err == nil {
		t.Fatal("unowned parent query accepted")
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner}
	command := agentproto.Command{CommandID: "query", Kind: agentproto.CommandThreadsRefresh, NativeBinding: &binding}
	if _, err := app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatalf("query frames=%d", len(frames))
	}
	id := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"data":[]}}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range observed.Events {
		if event.Kind == agentproto.EventNativeQueryCompleted && event.CommandID == command.CommandID {
			found = true
		}
	}
	if !found {
		t.Fatal("legitimate refresh not completed")
	}
}

func TestPrivateInterruptHasExactNativeResponse(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude} {
		t.Run(string(backend), func(t *testing.T) {
			app := privateTestApp(t, backend)
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "run"}
			command := agentproto.Command{CommandID: "stop", Kind: agentproto.CommandTurnInterrupt, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}}
			if _, err := app.prepareJournalCommand(command); err != nil {
				t.Fatal(err)
			}
			result, err := app.runtime.TranslateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			frames := flattenRuntimeCommandPhaseFrames(result.Phases)
			if len(frames) != 1 {
				t.Fatalf("frames=%d", len(frames))
			}
			request := nativeFrameRequestID(frames[0])
			if request == "" {
				t.Fatal("missing native request")
			}
			if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
				t.Fatal(err)
			}
			response := fmt.Sprintf(`{"id":%q,"result":{}}`, request)
			if backend == agentproto.BackendClaude {
				response = fmt.Sprintf(`{"type":"control_response","response":{"request_id":%q,"subtype":"success","response":{}}}`, request)
			}
			observed, err := app.runtime.ObserveServer([]byte(response))
			if err != nil {
				t.Fatal(err)
			}
			if len(observed.Events) != 1 {
				t.Fatalf("events %+v", observed.Events)
			}
			e := observed.Events[0]
			if e.Kind != agentproto.EventNativeControlResult || e.NativeBinding == nil || e.CommandID != command.CommandID || e.RequestID != request || e.ThreadID != "session" || e.TurnID != "turn" || e.Status != "accepted" {
				t.Fatalf("invalid stop response %+v", e)
			}
		})
	}
}

func TestPrivateQueryAndControlMissingResultStayUnknown(t *testing.T) {
	for _, kind := range []agentproto.CommandKind{agentproto.CommandThreadsRefresh, agentproto.CommandTurnInterrupt} {
		t.Run(string(kind), func(t *testing.T) {
			app := privateTestApp(t, agentproto.BackendCodex)
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner}
			if kind == agentproto.CommandTurnInterrupt {
				binding.ExecutionID = "run"
			}
			command := agentproto.Command{CommandID: "command", Kind: kind, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}}
			if _, err := app.prepareJournalCommand(command); err != nil {
				t.Fatal(err)
			}
			result, err := app.runtime.TranslateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			frames := flattenRuntimeCommandPhaseFrames(result.Phases)
			if len(frames) != 1 {
				t.Fatalf("frames=%d", len(frames))
			}
			id := nativeFrameRequestID(frames[0])
			if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
				t.Fatal(err)
			}
			observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q}`, id)))
			if err != nil {
				t.Fatal(err)
			}
			want := agentproto.EventNativeQueryCompleted
			if kind == agentproto.CommandTurnInterrupt {
				want = agentproto.EventNativeControlResult
			}
			found := false
			for _, event := range observed.Events {
				if event.Kind == want {
					found = true
					if event.Status != "unknown" {
						t.Fatalf("missing result claimed %s", event.Status)
					}
				}
			}
			if !found {
				t.Fatalf("missing response evidence: %+v", observed.Events)
			}
		})
	}
}

func TestPrivateStartNativeRejectionDoesNotReportAccepted(t *testing.T) {
	app := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: app.privateNative.snapshot.Owner, ExecutionID: "turn-owner"}
	command := agentproto.Command{CommandID: "start-rejected", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{CWD: app.privateNative.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}}}
	if _, err := app.prepareWorkCommand(command); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	frames := flattenRuntimeCommandPhaseFrames(result.Phases)
	if len(frames) != 1 {
		t.Fatal("missing frame")
	}
	id := nativeFrameRequestID(frames[0])
	if err = writeJournaledChildFrame(io.Discard, frames[0], app.runtime); err != nil {
		t.Fatal(err)
	}
	observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"error":{"code":-1,"message":"denied"}}`, id)))
	if err != nil {
		t.Fatal(err)
	}
	rejected := 0
	for _, event := range observed.Events {
		if event.Status == "accepted" || event.Kind == agentproto.EventNativeTurnBindingObserved {
			t.Fatalf("reported acceptance: %+v", event)
		}
		if event.Kind == agentproto.EventTurnCompleted && event.Status == "failed" {
			rejected++
			if event.NativeBinding == nil || *event.NativeBinding != binding || event.RequestID != id {
				t.Fatalf("bad rejection: %+v", event)
			}
		}
	}
	if rejected != 1 {
		t.Fatalf("missing rejection: %+v", observed.Events)
	}
}
