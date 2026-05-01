package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSendIMVideoToolRoutesByResolvedSurface(t *testing.T) {
	sender := &fakeToolSender{}
	app, _ := newToolServiceTestApp(t, sender)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	workspaceOne := t.TempDir()
	workspaceTwo := t.TempDir()
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceOne,
		WorkspaceKey:  workspaceOne,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		WorkspaceRoot: workspaceTwo,
		WorkspaceKey:  workspaceTwo,
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
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		GatewayID:        "app-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})
	startToolTestRemoteTurn(t, app, "surface-2", "inst-2", "thread-2", "turn-2")

	videoPath := filepath.Join(t.TempDir(), "demo.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, toolErr := app.sendIMVideoTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
		"path":               videoPath,
	})
	if toolErr != nil {
		t.Fatalf("sendIMVideoTool() error = %#v", toolErr)
	}
	if len(sender.videoCalls) != 1 {
		t.Fatalf("expected one video send call, got %#v", sender.videoCalls)
	}
	got := sender.videoCalls[0]
	if got.GatewayID != "app-2" || got.SurfaceSessionID != "surface-2" || got.ChatID != "chat-2" || got.ActorUserID != "user-2" {
		t.Fatalf("unexpected video send request: %#v", got)
	}
	if result["surface_session_id"] != "surface-2" || result["message_id"] != "msg-video" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}

func TestSendIMVideoToolRejectsInvalidPathAndDetachedSurface(t *testing.T) {
	sender := &fakeToolSender{}
	app, _ := newToolServiceTestApp(t, sender)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	workspaceOne := t.TempDir()
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceOne,
		WorkspaceKey:  workspaceOne,
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

	missingPath := filepath.Join(t.TempDir(), "missing.mp4")
	_, toolErr := app.sendIMVideoTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
		"path":               missingPath,
	})
	if toolErr == nil || toolErr.Code != "video_not_found" {
		t.Fatalf("expected video_not_found, got %#v", toolErr)
	}

	textPath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, toolErr = app.sendIMVideoTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
		"path":               textPath,
	})
	if toolErr == nil || toolErr.Code != "invalid_video_path" {
		t.Fatalf("expected invalid_video_path for non-mp4, got %#v", toolErr)
	}

	videoPath := filepath.Join(t.TempDir(), "demo.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, toolErr = app.sendIMVideoTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"path": videoPath,
	})
	if toolErr == nil || toolErr.Code != "current_turn_surface_unavailable" {
		t.Fatalf("expected missing current turn error, got %#v", toolErr)
	}
}

func TestSendIMVideoToolMapsUploadAndSendFailures(t *testing.T) {
	testCases := []struct {
		name     string
		sendFn   func(context.Context, feishu.IMVideoSendRequest) (feishu.IMVideoSendResult, error)
		wantCode string
	}{
		{
			name: "upload failure",
			sendFn: func(context.Context, feishu.IMVideoSendRequest) (feishu.IMVideoSendResult, error) {
				return feishu.IMVideoSendResult{}, &feishu.IMVideoSendError{
					Code: feishu.IMVideoSendErrorUploadFailed,
					Err:  errors.New("video too large"),
				}
			},
			wantCode: "upload_failed",
		},
		{
			name: "send failure",
			sendFn: func(context.Context, feishu.IMVideoSendRequest) (feishu.IMVideoSendResult, error) {
				return feishu.IMVideoSendResult{}, &feishu.IMVideoSendError{
					Code: feishu.IMVideoSendErrorSendFailed,
					Err:  errors.New("network error"),
				}
			},
			wantCode: "send_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeToolSender{videoSendFn: tc.sendFn}
			app, _ := newToolServiceTestApp(t, sender)
			if err := app.Bind(); err != nil {
				t.Fatalf("Bind() error = %v", err)
			}
			defer func() {
				_ = app.Shutdown(context.Background())
			}()

			workspaceRoot := t.TempDir()
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
			startToolTestRemoteTurn(t, app, "surface-1", "inst-1", "thread-1", "turn-1")

			videoPath := filepath.Join(t.TempDir(), "demo.mp4")
			if err := os.WriteFile(videoPath, []byte("fake-video"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, toolErr := app.sendIMVideoTool(withToolCallerInstanceID(context.Background(), "inst-1"), map[string]any{
				"path": videoPath,
			})
			if toolErr == nil || toolErr.Code != tc.wantCode {
				t.Fatalf("expected %s, got %#v", tc.wantCode, toolErr)
			}
		})
	}
}
