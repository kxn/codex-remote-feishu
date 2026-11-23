package wrapper

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func newTestWorkApp(t *testing.T, backend agentproto.Backend) *App {
	t.Helper()
	cwd := t.TempDir()
	app := New(Config{
		Backend: backend, InstanceID: "work-wrapper", Managed: true, Source: "headless", WorkspaceRoot: cwd,
		WorkLaunchSnapshot: &agentproto.RuntimeLaunchSnapshot{
			HostID: "test-host", Version: 1,
			Owner: agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 2, ControllerGeneration: 3, BotGeneration: 4},
			CWD:   cwd, InstructionBundle: agentproto.InstructionBundle{Revision: "instructions-v1", Text: "existing profile\nplatform contract\nrole instructions"},
		},
	})
	if app.workErr != nil || app.work == nil {
		t.Fatalf("create worker wrapper: %v", app.workErr)
	}
	t.Cleanup(func() { cleanupBackendRuntime(app.runtime) })
	return app
}

func testWorkCommand(app *App) agentproto.Command {
	binding := app.work.snapshot.Owner
	binding.AttemptID, binding.RunRef = "attempt-1", "run-1"
	return agentproto.Command{
		CommandID: "command-1", Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: "instructions-v1",
		Target:      agentproto.Target{CWD: app.work.snapshot.CWD, ExecutionMode: agentproto.PromptExecutionModeStartNew},
		Prompt:      agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "work input"}}},
		CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "openai", DeveloperInstruction: "command supplied content is not configuration"},
	}
}

func requireWorkProblem(t *testing.T, err error, code string) {
	t.Helper()
	var problem agentproto.ErrorInfo
	if !errors.As(err, &problem) || problem.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestWorkExecutionValidatesEveryOwnerAndGeneration(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendClaude)
	mutations := map[string]func(*agentproto.ExecutionBinding){
		"scope":                 func(b *agentproto.ExecutionBinding) { b.ScopeID = "another" },
		"context":               func(b *agentproto.ExecutionBinding) { b.ContextID = "another" },
		"role":                  func(b *agentproto.ExecutionBinding) { b.RoleID = "another" },
		"worker":                func(b *agentproto.ExecutionBinding) { b.WorkerSessionID = "another" },
		"session generation":    func(b *agentproto.ExecutionBinding) { b.SessionGeneration++ },
		"controller generation": func(b *agentproto.ExecutionBinding) { b.ControllerGeneration++ },
		"bot generation":        func(b *agentproto.ExecutionBinding) { b.BotGeneration++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			command := testWorkCommand(app)
			mutate(command.Execution)
			_, err := app.prepareWorkCommand(command)
			requireWorkProblem(t, err, "work_execution_owner_mismatch")
		})
	}
	for _, field := range []string{"execution", "attempt", "run_ref", "command", "version"} {
		t.Run(field, func(t *testing.T) {
			command := testWorkCommand(app)
			switch field {
			case "execution":
				command.Execution = nil
			case "attempt":
				command.Execution.AttemptID = ""
			case "run_ref":
				command.Execution.RunRef = ""
			case "command":
				command.CommandID = ""
			case "version":
				command.Execution.Version++
			}
			_, err := app.prepareWorkCommand(command)
			requireWorkProblem(t, err, "work_execution_binding_required")
		})
	}
}

func TestWorkExecutionCWDAndInstructionBoundary(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendCodex)
	command := testWorkCommand(app)
	originalInstruction := command.CodexResume.DeveloperInstruction
	prepared, err := app.prepareWorkCommand(command)
	if err != nil || prepared.CodexResume.DeveloperInstruction != app.work.snapshot.InstructionBundle.Text || command.CodexResume.DeveloperInstruction != originalInstruction {
		t.Fatalf("frozen instruction did not replace command policy safely: %v", err)
	}
	command.Target.ThreadID = "same-thread"
	command.InstructionRevision = "instructions-v2"
	_, err = app.prepareWorkCommand(command)
	requireWorkProblem(t, err, "work_instruction_rebuild_required")
	command = testWorkCommand(app)
	command.Target.CWD = filepath.Join(app.config.WorkspaceRoot, "new-worktree")
	_, err = app.prepareWorkCommand(command)
	requireWorkProblem(t, err, "work_execution_rebuild_required")
	command.Target.CWD = ""
	_, err = app.prepareWorkCommand(command)
	requireWorkProblem(t, err, "work_execution_cwd_required")
	command.Kind, command.InstructionRevision, command.CodexResume = agentproto.CommandTurnInterrupt, "", nil
	if _, err := app.prepareWorkCommand(command); err != nil {
		t.Fatalf("control command may use fixed launch cwd: %v", err)
	}
	command = testWorkCommand(app)
	command.CodexResume = nil
	_, err = app.prepareWorkCommand(command)
	requireWorkProblem(t, err, "work_codex_policy_required")
}

func TestWorkExecutionOwnershipUsesOnlyExactCommandID(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendClaude)
	first := testWorkCommand(app)
	if _, err := app.prepareWorkCommand(first); err != nil {
		t.Fatal(err)
	}
	second := testWorkCommand(app)
	second.CommandID, second.Execution.AttemptID, second.Execution.RunRef = "command-2", "attempt-2", "run-2"
	if _, err := app.prepareWorkCommand(second); err != nil {
		t.Fatal(err)
	}
	events := []agentproto.Event{
		{CommandID: "command-2", ThreadID: "shared-native-thread"},
		{CommandID: "command-1", ThreadID: "shared-native-thread"},
		{ThreadID: "shared-native-thread", Execution: second.Execution},
		{CommandID: "unknown", ThreadID: "shared-native-thread"},
	}
	app.work.annotateEvents(events)
	if events[0].Execution == nil || *events[0].Execution != *second.Execution || events[1].Execution == nil || *events[1].Execution != *first.Execution || events[2].Execution != nil || events[3].Execution != nil {
		t.Fatal("events were associated by native thread or newest attempt")
	}
	first.Execution.AttemptID = "mutated-after-registration"
	if events[1].Execution.AttemptID != "attempt-1" {
		t.Fatal("event binding shared mutable command memory")
	}
	_, err := app.prepareWorkCommand(first)
	requireWorkProblem(t, err, "work_command_binding_conflict")
}

func TestWorkExecutionLegacyAndHello(t *testing.T) {
	legacy := New(Config{Backend: agentproto.BackendClaude})
	if legacy.relayHello().WorkerProtocolVersion != 0 {
		t.Fatal("legacy wrapper advertised worker protocol")
	}
	if _, err := legacy.prepareWorkCommand(agentproto.Command{Kind: agentproto.CommandPromptSend}); err != nil {
		t.Fatal(err)
	}
	work := newTestWorkApp(t, agentproto.BackendClaude)
	if work.relayHello().WorkerProtocolVersion != 1 {
		t.Fatal("bound wrapper did not advertise binding protocol")
	}
	_, err := legacy.prepareWorkCommand(testWorkCommand(work))
	requireWorkProblem(t, err, "work_execution_not_supported")
	openCode := New(Config{Backend: agentproto.BackendOpenCode, Managed: true, Source: "headless", WorkspaceRoot: work.config.WorkspaceRoot, WorkLaunchSnapshot: work.config.WorkLaunchSnapshot})
	if openCode.workErr != nil || openCode.relayHello().WorkerProtocolVersion != 1 {
		t.Fatal("OpenCode did not advertise implemented owner validation")
	}
}

func TestLegacyWrapperRejectsExecutionBeforeProcessControl(t *testing.T) {
	server, _, acks, stdout, stderr, done := startWrapperClaudeRuntimeTestApp(t, "interrupt")
	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{CommandID: "bound-exit", Kind: agentproto.CommandProcessExit, Execution: &agentproto.ExecutionBinding{Version: 1}}); err != nil {
		t.Fatal(err)
	}
	var ack agentproto.CommandAck
	waitForAck(t, acks, 5*time.Second, func(received agentproto.CommandAck) bool {
		if received.CommandID != "bound-exit" {
			return false
		}
		ack = received
		return true
	}, stdout, stderr, done)
	if ack.Accepted || ack.Problem == nil || ack.Problem.Code != "work_execution_not_supported" {
		t.Fatalf("legacy accepted worker process control: %#v", ack)
	}
}

func TestWorkLaunchSnapshotReadAndFreeze(t *testing.T) {
	app := newTestWorkApp(t, agentproto.BackendClaude)
	path := filepath.Join(t.TempDir(), "launch.json")
	raw, err := json.Marshal(app.work.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := readWorkLaunchSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Owner != app.work.snapshot.Owner {
		t.Fatal("snapshot owner lost")
	}
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded.InstructionBundle.Revision != "instructions-v1" {
		t.Fatal("snapshot was not immutable after read")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readWorkLaunchSnapshot(path); err == nil {
			t.Fatal("accepted shared snapshot file")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		link := path + ".link"
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, err := readWorkLaunchSnapshot(link); err == nil {
			t.Fatal("accepted symlink snapshot")
		}
	}
	for _, invalid := range []string{string(raw) + ` {}`, `{"unknown":true}`, strings.Repeat(" ", maxWorkLaunchSnapshotBytes+1)} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWorkLaunchSnapshot(path); err == nil {
			t.Fatal("accepted invalid launch snapshot")
		}
	}
}

func TestWorkLaunchInstructionsAndSecretRemainFrozen(t *testing.T) {
	t.Setenv(config.ClaudeAppendSystemPromptEnv, "legacy profile")
	t.Setenv(config.WorkSessionTokenEnv, "test-secret-never-in-hello")
	t.Setenv(agentproto.TeamRelayCredentialEnv, "team-relay-secret")
	t.Setenv(config.WorkLaunchSnapshotEnv, "/private/snapshot.json")
	app := newTestWorkApp(t, agentproto.BackendClaude)
	app.config.WorkLaunchSnapshot.InstructionBundle.Text = "caller mutation"
	t.Setenv(config.WorkSessionTokenEnv, "later-token")
	frame, err := app.claudeBootstrapInitializeFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(frame), `existing profile\nplatform contract\nrole instructions`) || strings.Contains(string(frame), "caller mutation") {
		t.Fatal("initialize did not use frozen composed profile instruction")
	}
	_, env := app.buildClaudeChildLaunch(nil)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, config.WorkSessionTokenEnv+"=test-secret-never-in-hello") || strings.Contains(joined, "later-token") || strings.Contains(joined, config.WorkLaunchSnapshotEnv+"=") || strings.Contains(joined, "team-relay-secret") {
		t.Fatal("child environment did not isolate frozen session credential")
	}
	_, codexEnv := app.buildCodexChildLaunch(nil)
	if !strings.Contains(strings.Join(codexEnv, "\n"), config.WorkSessionTokenEnv+"=test-secret-never-in-hello") {
		t.Fatal("Codex child did not receive frozen session credential")
	}
	public, err := json.Marshal(app.relayHello())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "test-secret") || strings.Contains(string(frame), "test-secret") || strings.Contains(string(public), "team-relay-secret") {
		t.Fatal("session credential leaked into public protocol or prompt")
	}
	legacyEnv := (&App{}).applyWorkChildEnv(env)
	if strings.Contains(strings.Join(legacyEnv, "\n"), config.WorkSessionTokenEnv+"=") {
		t.Fatal("legacy child inherited a worker credential")
	}
}

func TestWorkCodexStartResumeAndFailureKeepInstructionAndOwner(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			app := newTestWorkApp(t, agentproto.BackendCodex)
			command := testWorkCommand(app)
			if resume {
				command.Target.ThreadID, command.Target.ExecutionMode = "native-existing", agentproto.PromptExecutionModeResumeExisting
			}
			result, err := app.runtime.TranslateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			var frame map[string]any
			if err := json.Unmarshal(result.Phases[0].OutboundToChild[0], &frame); err != nil {
				t.Fatal(err)
			}
			params := frame["params"].(map[string]any)
			if params["developerInstructions"] != app.work.snapshot.InstructionBundle.Text || params["modelProvider"] != "openai" {
				t.Fatal("native thread boundary did not receive frozen default profile instructions")
			}
			observed, err := app.runtime.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"error":{"message":"native refused"}}`, frame["id"])))
			if err != nil || len(observed.Events) != 1 || observed.Events[0].Execution == nil || *observed.Events[0].Execution != *command.Execution {
				t.Fatalf("native response lost exact command owner: %v", err)
			}
		})
	}
}
