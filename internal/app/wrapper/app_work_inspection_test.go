package wrapper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestWorkInspectionNativeChildHelper(t *testing.T) {
	if os.Getenv("WORK_INSPECTION_CHILD") != "1" {
		return
	}
	if os.Getenv("WORK_INSPECTION_BACKGROUND_WAIT") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	var outputMu sync.Mutex
	write := func(value any) {
		outputMu.Lock()
		defer outputMu.Unlock()
		raw, _ := json.Marshal(value)
		fmt.Println(string(raw))
	}
	var background *exec.Cmd
	defer func() {
		if background != nil {
			_ = background.Process.Kill()
		}
	}()
	reader := bufio.NewScanner(os.Stdin)
	for reader.Scan() {
		var message map[string]any
		if json.Unmarshal(reader.Bytes(), &message) != nil {
			continue
		}
		id := message["id"]
		method, _ := message["method"].(string)
		params, _ := message["params"].(map[string]any)
		switch method {
		case "initialize":
			write(map[string]any{"id": id, "result": map[string]any{"userAgent": "inspection-child", "protocolVersion": 1, "agentCapabilities": map[string]any{}}})
		case "session/new":
			write(map[string]any{"id": id, "result": map[string]any{"sessionId": "session"}})
		case "session/prompt":
			write(map[string]any{"id": id, "result": map[string]any{"stopReason": "end_turn"}})
		case "initialized":
		case "thread/start", "thread/resume":
			write(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "session", "cwd": "/workspace"}}})
		case "turn/start":
			write(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
			write(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "session", "turn": map[string]any{"id": "turn"}}})
			if os.Getenv("WORK_INSPECTION_START_BACKGROUND") == "1" {
				background = exec.Command(os.Args[0], "-test.run=^TestWorkInspectionNativeChildHelper$")
				background.Env = append(os.Environ(), "WORK_INSPECTION_BACKGROUND_WAIT=1")
				background.Stdout = io.Discard
				background.Stderr = io.Discard
				if err := background.Start(); err != nil {
					t.Fatal(err)
				}
				write(map[string]any{"method": "item/started", "params": map[string]any{"threadId": "session", "turnId": "turn", "item": map[string]any{"type": "commandExecution", "id": "background-tool", "command": "long-running-owned-command", "processId": "native-bg-1", "status": "inProgress"}}})
				owned := background
				go func() {
					_ = owned.Wait()
					write(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "session", "turnId": "turn", "item": map[string]any{"type": "commandExecution", "id": "background-tool", "processId": "native-bg-1", "status": "completed", "exitCode": 1}}})
				}()
			}
			write(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "session", "turn": map[string]any{"id": "turn", "status": "completed"}}})
		case "thread/read":
			write(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": params["threadId"], "status": map[string]any{"type": "idle"}}}})
		case "thread/goal/get":
			var goal any
			if os.Getenv("WORK_INSPECTION_ACTIVE_GOAL") == "1" {
				goal = map[string]any{"status": "active", "objective": "active"}
			}
			write(map[string]any{"id": id, "result": map[string]any{"goal": goal}})
		default:
			write(map[string]any{"id": id, "error": map[string]any{"message": "unsupported inspection method"}})
		}
	}
}

type inspectionHarness struct {
	app      *App
	writeCh  chan []byte
	terminal <-chan agentproto.Event
	errors   <-chan error
	command  agentproto.Command
}

func startInspectionHarness(t *testing.T, background bool) *inspectionHarness {
	return startInspectionHarnessBackend(t, background, agentproto.BackendCodex)
}

func startInspectionHarnessBackend(t *testing.T, background bool, backend agentproto.Backend) *inspectionHarness {
	t.Helper()
	base := newTestWorkApp(t, backend)
	cfg := base.config
	cfg.RuntimePaths.StateDir = t.TempDir()
	cfg.WorkLaunchSnapshot.JournalPath = filepath.Join(cfg.RuntimePaths.StateDir, "runtime.db")
	cfg.WorkLaunchSnapshot.CarrierIncarnation = "inspect-carrier"
	cfg.WorkLaunchSnapshot.InstanceGeneration = 1
	cfg.WorkLaunchSnapshot.ProcessGeneration = 1
	cfg.WorkLaunchSnapshot.LocationGeneration = 1
	app := New(cfg)
	if app.workErr != nil {
		t.Fatal(app.workErr)
	}
	t.Cleanup(func() { cleanupBackendRuntime(app.runtime) })
	child := exec.Command(os.Args[0], "-test.run=^TestWorkInspectionNativeChildHelper$")
	child.Env = append(os.Environ(), "WORK_INSPECTION_CHILD=1")
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
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := app.bindWorkProcess(child)
	if err != nil {
		_ = child.Process.Kill()
		t.Fatal(err)
	}
	var bootstrapped io.Reader
	if backend == agentproto.BackendOpenCode {
		rt := app.runtime.(*workExecutionRuntime).backendRuntime.(*opencodeBackendRuntime)
		bootstrapped, err = app.bootstrapOpenCodeACP(rt.translator, stdin, stdout, nil, nil)
	} else {
		bootstrapped, err = app.bootstrapHeadlessCodex(stdin, stdout, nil, nil)
	}
	if err != nil {
		_ = child.Process.Kill()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	writeCh := make(chan []byte, 16)
	terminal := make(chan agentproto.Event, 8)
	errorsCh := make(chan error, 16)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-writeCh:
				if err := writeChildFrameForRuntime(stdin, frame, app.runtime, nil, nil, nil); err != nil {
					errorsCh <- err
					return
				}
			}
		}
	}()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		reader := bufio.NewScanner(bootstrapped)
		for reader.Scan() {
			result, err := app.runtime.ObserveServer(reader.Bytes())
			if err != nil {
				continue
			}
			if len(result.Events) > 0 {
				events, e := app.work.journal.PersistEvents(result.Events)
				if e != nil {
					errorsCh <- e
					return
				}
				for _, event := range events {
					if event.Kind == agentproto.EventTurnCompleted {
						terminal <- event
					}
				}
			}
			for _, frame := range result.OutboundToChild {
				select {
				case writeCh <- frame:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		_ = scope.StopChildren(context.Background())
		scope.MarkControlledStop()
		cancel()
		_ = stdin.Close()
		_ = child.Wait()
		scope.MarkWaitObserved()
		app.work.inspection.mu.Lock()
		app.work.inspection.dirty = true
		app.work.inspection.mu.Unlock()
		_ = app.work.inspection.persist(app.work.journal, app.work.snapshot.Owner)
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
			_ = child.Process.Kill()
		}
	})
	command := testWorkCommand(app)
	if duplicate, err := app.prepareJournalCommand(command); err != nil || duplicate {
		t.Fatalf("journal prepare: %v %v", duplicate, err)
	}
	result, err := app.runtime.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range flattenRuntimeCommandPhaseFrames(result.Phases) {
		writeCh <- frame
	}
	select {
	case <-terminal:
	case err := <-errorsCh:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("fake native turn did not complete")
	}
	return &inspectionHarness{app: app, writeCh: writeCh, terminal: terminal, errors: errorsCh, command: command}
}

func (h *inspectionHarness) inspect(t *testing.T, id string, ownerOnly, stop bool) agentproto.RuntimeInspectionResult {
	t.Helper()
	command := h.command
	command.CommandID = id
	command.Kind = agentproto.CommandRuntimeInspect
	command.Target.ThreadID = "session"
	command.Target.TurnID = "turn"
	if h.app.config.Backend == agentproto.BackendOpenCode {
		command.Target.TurnID = "opencode-turn-" + h.command.CommandID
	}
	command.RuntimeInspection = &agentproto.RuntimeInspectionRequest{Version: 1, ExpectedInstanceGeneration: 1, ExpectedProcessGeneration: 1, ExpectedLocationGeneration: 1, SubjectCommandID: h.command.CommandID, StopBackground: stop}
	if ownerOnly {
		owner := h.app.work.snapshot.Owner
		command.Execution = &owner
		command.Target.ThreadID = ""
		command.Target.TurnID = ""
	}
	if _, err := h.app.prepareWorkCommand(command); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event := h.app.inspectNative(ctx, command, h.writeCh)
	if event.RuntimeInspection == nil {
		t.Fatal("missing typed inspection response")
	}
	return *event.RuntimeInspection
}

func (h *inspectionHarness) request(ownerOnly bool) workruntime.InspectionRequest {
	owner := h.app.work.snapshot.Owner
	binding := *h.command.Execution
	session, turn := "session", "turn"
	if ownerOnly {
		binding = owner
		session, turn = "", ""
	}
	return workruntime.InspectionRequest{SubjectCommandID: h.command.CommandID, Carrier: workstore.CarrierRegistration{Incarnation: "inspect-carrier", OriginInstanceID: h.app.config.InstanceID, Owner: owner, Backend: work.Backend(h.app.config.Backend), InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}, Binding: binding, SessionID: session, TurnID: turn, LocationGeneration: 1}
}

func TestManagedCollectorUsesActualPipesAndFixedRPCs(t *testing.T) {
	h := startInspectionHarness(t, false)
	result := h.inspect(t, "inspect-normal", false, false)
	if result.Problem != nil {
		t.Fatalf("collector problem: %#v", result.Problem)
	}
	observation, err := workruntime.ObserveRuntimeInspection(h.request(false), result)
	if err != nil || !observation.NativeStopped || !observation.ContinuationStopped || !observation.BackgroundWorkStopped {
		t.Fatalf("foreground native work did not produce actual quiescence evidence: %#v %v", observation, err)
	}
	if len(result.Native.Request) == 0 || len(result.Native.Response) == 0 || len(result.Continuation.Response) == 0 || len(result.Background.Evidence) == 0 {
		t.Fatal("collector returned booleans without real RPC/host evidence")
	}
	result = h.inspect(t, "inspect-owner", true, false)
	observation, err = workruntime.ObserveRuntimeInspection(h.request(true), result)
	if err != nil || !observation.NativeStopped || !observation.ContinuationStopped || !observation.BackgroundWorkStopped {
		t.Fatalf("owner-only idle inspection failed: %#v %v", observation, err)
	}
}

func TestManagedOpenCodeCollectorUsesExactACPResponseAndHostProcessScope(t *testing.T) {
	h := startInspectionHarnessBackend(t, false, agentproto.BackendOpenCode)
	result := h.inspect(t, "inspect-opencode", false, false)
	request := h.request(false)
	request.TurnID = "opencode-turn-" + h.command.CommandID
	observation, err := workruntime.ObserveRuntimeInspection(request, result)
	if err != nil {
		t.Fatal(err)
	}
	if err = observation.ValidateStopped(request); err != nil {
		t.Fatal(err)
	}
	if result.StreamTerminal == nil || result.StreamTerminal.CommandID != h.command.CommandID || result.StreamTerminal.Journal == nil {
		t.Fatal("ACP completion lost exact native command or durable boundary")
	}
}

func TestManagedCollectorTracksAndStopsOwnedBackgroundProcess(t *testing.T) {
	h := startInspectionHarness(t, true)
	result := h.inspect(t, "inspect-background", false, false)
	observation, err := workruntime.ObserveRuntimeInspection(h.request(false), result)
	if err != nil {
		t.Fatal(err)
	}
	if observation.BackgroundWorkStopped || len(result.Background.RunningProcessIDs) == 0 || len(result.Background.PendingTaskIDs) == 0 {
		t.Fatal("live owned background process/task was assumed absent")
	}
	result = h.inspect(t, "inspect-stop-background", false, true)
	observation, err = workruntime.ObserveRuntimeInspection(h.request(false), result)
	if err != nil || !observation.BackgroundWorkStopped {
		t.Fatalf("actual owned background stop did not reconcile: %#v %v", result.Background, err)
	}
}
