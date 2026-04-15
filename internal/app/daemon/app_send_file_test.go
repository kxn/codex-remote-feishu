package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHandleSendIMFileCommandSendsFileToCurrentSurface(t *testing.T) {
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

	events := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filePath,
	})

	if len(sender.calls) != 1 {
		t.Fatalf("expected one send call, got %#v", sender.calls)
	}
	if sender.calls[0].SurfaceSessionID != "surface-1" || sender.calls[0].ChatID != "chat-1" || sender.calls[0].ActorUserID != "user-1" {
		t.Fatalf("unexpected send call: %#v", sender.calls[0])
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_sent" {
		t.Fatalf("expected send success notice, got %#v", events)
	}
}

func TestHandleSendIMFileCommandRejectsMissingFileAndDetachedSurface(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})

	missing := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filepath.Join(t.TempDir(), "missing.txt"),
	})
	if len(missing) != 1 || missing[0].Notice == nil || missing[0].Notice.Code != "send_file_not_found" {
		t.Fatalf("expected missing-file notice, got %#v", missing)
	}

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	detached := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-unknown",
		LocalPath:        filePath,
	})
	if len(detached) != 1 || detached[0].Notice == nil || detached[0].Notice.Code != "send_file_unavailable" {
		t.Fatalf("expected detached-surface notice, got %#v", detached)
	}
}

func TestHandleSendIMFileCommandMapsUploadAndSendFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "upload failed",
			err:      &feishu.IMFileSendError{Code: feishu.IMFileSendErrorUploadFailed, Err: errors.New("upload failed")},
			wantCode: "send_file_upload_failed",
		},
		{
			name:     "send failed",
			err:      &feishu.IMFileSendError{Code: feishu.IMFileSendErrorSendFailed, Err: errors.New("send failed")},
			wantCode: "send_file_failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeToolFileSender{
				sendFn: func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
					return feishu.IMFileSendResult{}, tc.err
				},
			}
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

			events := app.handleSendIMFileCommand(control.DaemonCommand{
				Kind:             control.DaemonCommandSendIMFile,
				SurfaceSessionID: "surface-1",
				LocalPath:        filePath,
			})
			if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != tc.wantCode {
				t.Fatalf("expected %s notice, got %#v", tc.wantCode, events)
			}
		})
	}
}

func TestHandleSendIMFileCommandReleasesAppLockAndUsesDeadline(t *testing.T) {
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

	sender.sendFn = func(ctx context.Context, req feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
		assertDaemonContextHasDeadlineWithin(t, ctx, sendIMFileCommandTimeout)

		lockAcquired := make(chan struct{})
		go func() {
			app.mu.Lock()
			close(lockAcquired)
			app.mu.Unlock()
		}()
		select {
		case <-lockAcquired:
		case <-time.After(time.Second):
			t.Fatal("app mutex remained locked during SendIMFile")
		}

		return feishu.IMFileSendResult{
			GatewayID:        req.GatewayID,
			SurfaceSessionID: req.SurfaceSessionID,
			FileName:         filepath.Base(req.Path),
			FileKey:          "file-key",
			MessageID:        "msg-file",
		}, nil
	}

	events := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filePath,
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_sent" {
		t.Fatalf("expected send success notice, got %#v", events)
	}
}

func TestHandleActionPathPickerConfirmSendFileDoesNotDeadlock(t *testing.T) {
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

	waitAction := func(name string, fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			fn()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s timed out (possible deadlock)", name)
		}
	}

	waitAction("open send file picker", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionSendFile,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
		})
	})
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 || surfaces[0].ActivePathPicker == nil {
		t.Fatalf("expected active path picker, got %#v", surfaces)
	}
	pickerID := surfaces[0].ActivePathPicker.PickerID
	if pickerID == "" {
		t.Fatalf("expected picker id")
	}

	waitAction("select file", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionPathPickerSelect,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			PickerID:         pickerID,
			PickerEntry:      filepath.Base(filePath),
		})
	})
	waitAction("confirm picker and send file", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionPathPickerConfirm,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			PickerID:         pickerID,
		})
	})
	waitAction("follow-up status action", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionStatus,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
		})
	})

	if len(sender.calls) != 1 {
		t.Fatalf("expected one send call after confirm, got %#v", sender.calls)
	}
}

func assertDaemonContextHasDeadlineWithin(t *testing.T, ctx context.Context, max time.Duration) {
	t.Helper()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("expected future deadline, got %s", deadline)
	}
	if remaining > max+time.Second {
		t.Fatalf("expected deadline within %s, got remaining %s", max, remaining)
	}
}
