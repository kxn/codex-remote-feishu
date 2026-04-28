package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type reentrantAppLockRelaySender struct {
	app *App

	mu          sync.Mutex
	instanceIDs []string
	commands    []agentproto.Command
	err         error
}

func (s *reentrantAppLockRelaySender) send(instanceID string, command agentproto.Command) error {
	s.mu.Lock()
	s.instanceIDs = append(s.instanceIDs, instanceID)
	s.commands = append(s.commands, command)
	s.mu.Unlock()

	locked := make(chan struct{})
	go func() {
		s.app.mu.Lock()
		s.app.mu.Unlock()
		close(locked)
	}()

	select {
	case <-locked:
		return nil
	case <-time.After(250 * time.Millisecond):
		err := errors.New("relay send could not reacquire app lock")
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		return err
	}
}

func (s *reentrantAppLockRelaySender) snapshot() ([]string, []agentproto.Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.instanceIDs...), append([]agentproto.Command(nil), s.commands...), s.err
}

func TestOnHelloReleasesAppLockDuringStartupThreadsRefreshSend(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	sender := &reentrantAppLockRelaySender{app: app}
	app.sendAgentCommand = sender.send

	done := make(chan struct{})
	go func() {
		app.onHello(context.Background(), agentproto.Hello{
			Instance: agentproto.InstanceHello{
				InstanceID:    "inst-1",
				DisplayName:   "workspace",
				WorkspaceRoot: "/tmp/workspace",
				WorkspaceKey:  "/tmp/workspace",
				ShortName:     "workspace",
				Source:        "headless",
				Managed:       true,
				PID:           1234,
			},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onHello timed out while relay send reentered app lock")
	}

	instanceIDs, commands, err := sender.snapshot()
	if err != nil {
		t.Fatalf("onHello should release app lock during startup threads.refresh: %v", err)
	}
	if len(instanceIDs) != 1 || instanceIDs[0] != "inst-1" {
		t.Fatalf("unexpected relay target: %#v", instanceIDs)
	}
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("unexpected relay commands: %#v", commands)
	}
}

func TestOnHelloWithoutThreadsRefreshSkipsStartupRefreshAndSettlesRound(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})

	var commands []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		commands = append(commands, command)
		return nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-claude",
			DisplayName:   "workspace",
			WorkspaceRoot: "/tmp/workspace",
			WorkspaceKey:  "/tmp/workspace",
			ShortName:     "workspace",
			Backend:       agentproto.BackendClaude,
			Source:        "headless",
			Managed:       true,
			PID:           1234,
		},
		CapabilitiesDeclared: true,
		Capabilities:         agentproto.Capabilities{},
	})

	if len(commands) != 0 {
		t.Fatalf("unexpected relay commands: %#v", commands)
	}
	inst := app.service.Instance("inst-claude")
	if inst == nil {
		t.Fatal("expected instance after hello")
	}
	if inst.Backend != agentproto.BackendClaude {
		t.Fatalf("instance backend = %q, want %q", inst.Backend, agentproto.BackendClaude)
	}
	if state.InstanceSupportsThreadsRefresh(inst) {
		t.Fatalf("expected instance to skip threads.refresh capability, got %#v", inst.Capabilities)
	}
	app.mu.Lock()
	seen := app.surfaceResumeRuntime.startupRefreshSeen
	pending := len(app.surfaceResumeRuntime.startupRefreshPending)
	complete := app.initialThreadsRefreshRoundCompleteLocked()
	app.mu.Unlock()
	if !seen || pending != 0 || !complete {
		t.Fatalf("expected startup refresh round to settle without refresh command, seen=%t pending=%d complete=%t", seen, pending, complete)
	}
}

func TestOnHelloRespectsExplicitClaudeCapabilityAdvertisement(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-claude-explicit",
			DisplayName:   "workspace",
			WorkspaceRoot: "/tmp/workspace",
			WorkspaceKey:  "/tmp/workspace",
			ShortName:     "workspace",
			Backend:       agentproto.BackendClaude,
			Source:        "headless",
			Managed:       true,
			PID:           1234,
		},
		CapabilitiesDeclared: true,
		Capabilities:         agentproto.Capabilities{},
	})

	inst := app.service.Instance("inst-claude-explicit")
	if inst == nil {
		t.Fatal("expected instance after hello")
	}
	if !inst.CapabilitiesDeclared {
		t.Fatalf("expected explicit capability advertisement to be preserved, got %#v", inst)
	}
	caps := state.EffectiveInstanceCapabilities(inst)
	if caps.ThreadsRefresh || caps.RequestRespond || caps.SessionCatalog || caps.ResumeByThreadID || caps.RequiresCWDForResume || caps.VSCodeMode {
		t.Fatalf("expected explicit zero capabilities to stay zero, got %#v", caps)
	}
}

func TestHandleCronHelloReleasesAppLockDuringPromptSend(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	sender := &reentrantAppLockRelaySender{app: app}
	app.sendAgentCommand = sender.send

	instanceID := cronrt.InstancePrefix + "job-1"
	app.cronRuntime.runs[instanceID] = &cronrt.RunState{
		InstanceID:   instanceID,
		WorkspaceKey: "/tmp/workspace",
		Prompt:       "say hello",
	}

	done := make(chan struct{})
	var handled bool
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		handled = app.handleCronHelloLocked(context.Background(), agentproto.Hello{
			Instance: agentproto.InstanceHello{
				InstanceID: instanceID,
				PID:        4321,
			},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleCronHelloLocked timed out while relay send reentered app lock")
	}

	_, commands, err := sender.snapshot()
	if err != nil {
		t.Fatalf("cron hello should release app lock during hidden prompt send: %v", err)
	}
	if !handled {
		t.Fatalf("expected cron hello to be handled")
	}
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandPromptSend {
		t.Fatalf("unexpected cron relay commands: %#v", commands)
	}
	if run := app.cronRuntime.runs[instanceID]; run == nil || run.CommandID == "" {
		t.Fatalf("expected cron run command id to be recorded, got %#v", run)
	}
}

func TestThreadHistoryDaemonCommandLockedReleasesAppLockDuringRelaySend(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	sender := &reentrantAppLockRelaySender{app: app}
	app.sendAgentCommand = sender.send

	done := make(chan struct{})
	var events []eventcontract.Event
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		events = app.handleThreadHistoryDaemonCommandLocked(control.DaemonCommand{
			Kind:             control.DaemonCommandThreadHistoryRead,
			SurfaceSessionID: "surface-1",
			InstanceID:       "inst-1",
			ThreadID:         "thread-1",
			SourceMessageID:  "msg-1",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleThreadHistoryDaemonCommandLocked timed out while relay send reentered app lock")
	}

	_, commands, err := sender.snapshot()
	if err != nil {
		t.Fatalf("thread history send should release app lock: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no immediate UI events on success, got %#v", events)
	}
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandThreadHistoryRead {
		t.Fatalf("unexpected history relay commands: %#v", commands)
	}
	if len(app.pendingThreadHistoryReads) != 1 {
		t.Fatalf("expected pending history request to be recorded, got %#v", app.pendingThreadHistoryReads)
	}
}

func TestHandleSendIMFileCommandLockedReleasesAppLockDuringSend(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	sendErr := error(nil)
	sender.sendFn = func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
		locked := make(chan struct{})
		go func() {
			app.mu.Lock()
			app.mu.Unlock()
			close(locked)
		}()

		select {
		case <-locked:
			return feishu.IMFileSendResult{
				GatewayID:        "app-1",
				SurfaceSessionID: "surface-1",
				FileName:         "report.txt",
				FileKey:          "file-key",
				MessageID:        "msg-file",
			}, nil
		case <-time.After(250 * time.Millisecond):
			sendErr = errors.New("im file send could not reacquire app lock")
			return feishu.IMFileSendResult{}, sendErr
		}
	}

	done := make(chan struct{})
	var events []eventcontract.Event
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		events = app.handleSendIMFileCommandLocked(control.DaemonCommand{
			Kind:             control.DaemonCommandSendIMFile,
			SurfaceSessionID: "surface-1",
			LocalPath:        filePath,
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleSendIMFileCommandLocked timed out while IM file send reentered app lock")
	}

	if sendErr != nil {
		t.Fatalf("file send should release app lock during IM send: %v", sendErr)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_sent" {
		t.Fatalf("expected send success notice, got %#v", events)
	}
}

func TestSyncFeishuTimeSensitiveLockedReleasesAppLockDuringGatewayApply(t *testing.T) {
	gateway := &reentrantAppLockGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	gateway.app = app

	surfaceID := "feishu:app-1:user:ou_user-1"
	app.service.MaterializeSurface(surfaceID, "app-1", "oc_p2p-1", "ou_user-1")
	found := false
	for _, surface := range app.service.Surfaces() {
		if surface == nil || surface.SurfaceSessionID != surfaceID {
			continue
		}
		surface.PendingRequests = map[string]*state.RequestPromptRecord{
			"req-1": {RequestID: "req-1"},
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("expected materialized surface")
	}

	done := make(chan struct{})
	go func() {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.syncFeishuTimeSensitiveLocked(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("syncFeishuTimeSensitiveLocked timed out while gateway apply reentered app lock")
	}

	operations, err := gateway.snapshot()
	if err != nil {
		t.Fatalf("time-sensitive sync should release app lock during gateway apply: %v", err)
	}
	if len(operations) != 1 || operations[0].Kind != feishu.OperationSetTimeSensitive {
		t.Fatalf("unexpected gateway operations: %#v", operations)
	}
}
