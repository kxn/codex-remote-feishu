package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

type fakeToolSender struct {
	fileSendFn  func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error)
	imageSendFn func(context.Context, feishu.IMImageSendRequest) (feishu.IMImageSendResult, error)
	fileCalls   []feishu.IMFileSendRequest
	imageCalls  []feishu.IMImageSendRequest
}

type fakeToolFileSender struct {
	sendFn func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error)
	calls  []feishu.IMFileSendRequest
}

func (f *fakeToolSender) Start(context.Context, feishu.ActionHandler) error { return nil }
func (f *fakeToolSender) Apply(context.Context, []feishu.Operation) error   { return nil }
func (f *fakeToolSender) SendIMFile(ctx context.Context, req feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
	f.fileCalls = append(f.fileCalls, req)
	if f.fileSendFn != nil {
		return f.fileSendFn(ctx, req)
	}
	return feishu.IMFileSendResult{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		FileName:         filepath.Base(req.Path),
		FileKey:          "file-key",
		MessageID:        "msg-file",
	}, nil
}

func (f *fakeToolSender) SendIMImage(ctx context.Context, req feishu.IMImageSendRequest) (feishu.IMImageSendResult, error) {
	f.imageCalls = append(f.imageCalls, req)
	if f.imageSendFn != nil {
		return f.imageSendFn(ctx, req)
	}
	return feishu.IMImageSendResult{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		ImageName:        filepath.Base(req.Path),
		ImageKey:         "image-key",
		MessageID:        "msg-image",
	}, nil
}

func (f *fakeToolFileSender) Start(context.Context, feishu.ActionHandler) error { return nil }
func (f *fakeToolFileSender) Apply(context.Context, []feishu.Operation) error   { return nil }
func (f *fakeToolFileSender) SendIMFile(ctx context.Context, req feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
	f.calls = append(f.calls, req)
	if f.sendFn != nil {
		return f.sendFn(ctx, req)
	}
	return feishu.IMFileSendResult{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		FileName:         filepath.Base(req.Path),
		FileKey:          "file-key",
		MessageID:        "msg-file",
	}, nil
}

func newToolServiceTestApp(t *testing.T, gateway feishu.Gateway) (*App, relayruntime.Paths) {
	t.Helper()
	stateDir := t.TempDir()
	paths := relayruntime.Paths{StateDir: stateDir, ToolServiceFile: filepath.Join(stateDir, "tool-service.json")}
	app := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: paths})
	app.SetToolRuntime(ToolRuntimeConfig{ListenAddr: "127.0.0.1:0", StateFile: paths.ToolServiceFile})
	return app, paths
}

func TestToolRuntimeRequiresBearerAndPublishesMCPState(t *testing.T) {
	app, paths := newToolServiceTestApp(t, nil)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method: http.MethodPost,
		Body:   toolMCPInitializeRequestBody(),
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without bearer, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method: http.MethodPost,
		Token:  app.toolRuntime.BearerToken,
		Body:   toolMCPInitializeRequestBody(),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected initialize success, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Header().Get("Mcp-Session-Id")) == "" {
		t.Fatalf("expected session id header, got headers=%v", rec.Header())
	}
	infoRaw, err := os.ReadFile(paths.ToolServiceFile)
	if err != nil {
		t.Fatalf("read tool service file: %v", err)
	}
	var info toolServiceInfo
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		t.Fatalf("unmarshal tool service file: %v", err)
	}
	if info.Token != app.toolRuntime.BearerToken {
		t.Fatalf("unexpected tool service file token: %#v", info)
	}
	if info.URL == "" || info.Protocol != "mcp" || info.Transport != "streamable_http" {
		t.Fatalf("unexpected tool service file: %#v", info)
	}
	if info.ManifestURL != "" || info.CallURL != "" {
		t.Fatalf("expected deprecated custom endpoints to be empty, got %#v", info)
	}
}

func TestResolveSurfaceContextToolRejectsVSCodeModeAndResolvesNormalMode(t *testing.T) {
	app, _ := newToolServiceTestApp(t, nil)
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
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-normal",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	result, toolErr := app.resolveSurfaceContextTool(map[string]any{"surface_session_id": "surface-normal"})
	if toolErr != nil {
		t.Fatalf("resolveSurfaceContextTool() error = %#v", toolErr)
	}
	workspaceRootValue, _ := result["workspace_root"].(string)
	if !testutil.SamePath(workspaceRootValue, workspaceRoot) || result["product_mode"] != "normal" {
		t.Fatalf("unexpected resolved context: %#v", result)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		Text:             "/mode vscode",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-1",
	})
	_, toolErr = app.resolveSurfaceContextTool(map[string]any{"surface_session_id": "surface-vscode"})
	if toolErr == nil || toolErr.Code != "surface_mode_unsupported" {
		t.Fatalf("expected vscode mode rejection, got %#v", toolErr)
	}
}

func TestSendIMFileToolRoutesByResolvedSurface(t *testing.T) {
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

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, toolErr := app.sendIMFileTool(context.Background(), map[string]any{
		"surface_session_id": "surface-2",
		"path":               filePath,
	})
	if toolErr != nil {
		t.Fatalf("sendIMFileTool() error = %#v", toolErr)
	}
	if len(sender.fileCalls) != 1 {
		t.Fatalf("expected one send call, got %#v", sender.fileCalls)
	}
	if sender.fileCalls[0].GatewayID != "app-2" || sender.fileCalls[0].ChatID != "chat-2" || sender.fileCalls[0].ActorUserID != "user-2" {
		t.Fatalf("unexpected routed send call: %#v", sender.fileCalls[0])
	}
	if result["surface_session_id"] != "surface-2" || result["message_id"] != "msg-file" || result["file_name"] != "report.txt" {
		t.Fatalf("unexpected send result: %#v", result)
	}
}

func TestSendIMFileToolRejectsInvalidAndDetachedSurface(t *testing.T) {
	sender := &fakeToolSender{}
	app, _ := newToolServiceTestApp(t, sender)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, toolErr := app.sendIMFileTool(context.Background(), map[string]any{
		"surface_session_id": "missing-surface",
		"path":               filePath,
	})
	if toolErr == nil || toolErr.Code != "surface_not_found" {
		t.Fatalf("expected invalid surface rejection, got %#v", toolErr)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-detached",
		GatewayID:        "app-1",
		ChatID:           "chat-detached",
		ActorUserID:      "user-detached",
		Text:             "hello",
	})
	_, toolErr = app.sendIMFileTool(context.Background(), map[string]any{
		"surface_session_id": "surface-detached",
		"path":               filePath,
	})
	if toolErr == nil || toolErr.Code != "surface_not_attached" {
		t.Fatalf("expected detached surface rejection, got %#v", toolErr)
	}
}

func TestSendIMFileToolMapsUploadAndSendFailures(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	testCases := []struct {
		name     string
		sendFn   func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error)
		wantCode string
	}{
		{
			name: "upload failure",
			sendFn: func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
				return feishu.IMFileSendResult{}, &feishu.IMFileSendError{
					Code: feishu.IMFileSendErrorUploadFailed,
					Err:  errors.New("too large"),
				}
			},
			wantCode: "upload_failed",
		},
		{
			name: "send failure",
			sendFn: func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
				return feishu.IMFileSendResult{}, &feishu.IMFileSendError{
					Code: feishu.IMFileSendErrorSendFailed,
					Err:  errors.New("network error"),
				}
			},
			wantCode: "send_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeToolSender{fileSendFn: tc.sendFn}
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

			_, toolErr := app.sendIMFileTool(context.Background(), map[string]any{
				"surface_session_id": "surface-1",
				"path":               filePath,
			})
			if toolErr == nil || toolErr.Code != tc.wantCode {
				t.Fatalf("expected %s, got %#v", tc.wantCode, toolErr)
			}
		})
	}
}

func TestSendIMImageToolRoutesByResolvedSurface(t *testing.T) {
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

	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, toolErr := app.sendIMImageTool(context.Background(), map[string]any{
		"surface_session_id": "surface-2",
		"path":               imagePath,
	})
	if toolErr != nil {
		t.Fatalf("sendIMImageTool() error = %#v", toolErr)
	}
	if len(sender.imageCalls) != 1 {
		t.Fatalf("expected one image send call, got %#v", sender.imageCalls)
	}
	if sender.imageCalls[0].GatewayID != "app-2" || sender.imageCalls[0].ChatID != "chat-2" || sender.imageCalls[0].ActorUserID != "user-2" {
		t.Fatalf("unexpected routed image send call: %#v", sender.imageCalls[0])
	}
	if result["surface_session_id"] != "surface-2" || result["message_id"] != "msg-image" || result["image_name"] != "preview.png" {
		t.Fatalf("unexpected image send result: %#v", result)
	}
}

func TestSendIMImageToolRejectsInvalidPathAndDetachedSurface(t *testing.T) {
	sender := &fakeToolSender{}
	app, _ := newToolServiceTestApp(t, sender)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	textPath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, toolErr := app.sendIMImageTool(context.Background(), map[string]any{
		"surface_session_id": "missing-surface",
		"path":               textPath,
	})
	if toolErr == nil || toolErr.Code != "invalid_image_path" {
		t.Fatalf("expected invalid image path rejection before surface lookup, got %#v", toolErr)
	}

	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-detached",
		GatewayID:        "app-1",
		ChatID:           "chat-detached",
		ActorUserID:      "user-detached",
		Text:             "hello",
	})
	_, toolErr = app.sendIMImageTool(context.Background(), map[string]any{
		"surface_session_id": "surface-detached",
		"path":               imagePath,
	})
	if toolErr == nil || toolErr.Code != "surface_not_attached" {
		t.Fatalf("expected detached surface rejection, got %#v", toolErr)
	}
}

func TestSendIMImageToolMapsUploadAndSendFailures(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	testCases := []struct {
		name     string
		sendFn   func(context.Context, feishu.IMImageSendRequest) (feishu.IMImageSendResult, error)
		wantCode string
	}{
		{
			name: "upload failure",
			sendFn: func(context.Context, feishu.IMImageSendRequest) (feishu.IMImageSendResult, error) {
				return feishu.IMImageSendResult{}, &feishu.IMImageSendError{
					Code: feishu.IMImageSendErrorUploadFailed,
					Err:  errors.New("bad image"),
				}
			},
			wantCode: "upload_failed",
		},
		{
			name: "send failure",
			sendFn: func(context.Context, feishu.IMImageSendRequest) (feishu.IMImageSendResult, error) {
				return feishu.IMImageSendResult{}, &feishu.IMImageSendError{
					Code: feishu.IMImageSendErrorSendFailed,
					Err:  errors.New("network error"),
				}
			},
			wantCode: "send_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeToolSender{imageSendFn: tc.sendFn}
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

			_, toolErr := app.sendIMImageTool(context.Background(), map[string]any{
				"surface_session_id": "surface-1",
				"path":               imagePath,
			})
			if toolErr == nil || toolErr.Code != tc.wantCode {
				t.Fatalf("expected %s, got %#v", tc.wantCode, toolErr)
			}
		})
	}
}
