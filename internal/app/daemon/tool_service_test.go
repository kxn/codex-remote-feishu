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
	toolruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/toolruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

type fakeToolSender struct {
	fileSendFn    func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error)
	imageSendFn   func(context.Context, feishu.IMImageSendRequest) (feishu.IMImageSendResult, error)
	videoSendFn   func(context.Context, feishu.IMVideoSendRequest) (feishu.IMVideoSendResult, error)
	commentReadFn func(context.Context, feishu.DriveFileCommentReadRequest) (feishu.DriveFileCommentReadResult, error)
	fileCalls     []feishu.IMFileSendRequest
	imageCalls    []feishu.IMImageSendRequest
	videoCalls    []feishu.IMVideoSendRequest
	commentCalls  []feishu.DriveFileCommentReadRequest
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

func (f *fakeToolSender) SendIMVideo(ctx context.Context, req feishu.IMVideoSendRequest) (feishu.IMVideoSendResult, error) {
	f.videoCalls = append(f.videoCalls, req)
	if f.videoSendFn != nil {
		return f.videoSendFn(ctx, req)
	}
	return feishu.IMVideoSendResult{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		VideoName:        filepath.Base(req.Path),
		FileKey:          "video-key",
		MessageID:        "msg-video",
	}, nil
}

func (f *fakeToolSender) ReadDriveFileComments(ctx context.Context, req feishu.DriveFileCommentReadRequest) (feishu.DriveFileCommentReadResult, error) {
	f.commentCalls = append(f.commentCalls, req)
	if f.commentReadFn != nil {
		return f.commentReadFn(ctx, req)
	}
	return feishu.DriveFileCommentReadResult{
		GatewayID:        req.GatewayID,
		FileToken:        req.FileToken,
		FileType:         req.FileType,
		StatsScope:       "returned_comments_page",
		CommentCount:     1,
		ReplyCount:       0,
		InteractionCount: 1,
		Comments: []feishu.DriveFileCommentEntry{
			{
				CommentID: "comment-1",
				UserID:    "ou_user_1",
				Replies: []feishu.DriveFileCommentReplyItem{
					{
						ReplyID: "reply-1",
						UserID:  "ou_user_1",
						Text:    "please update the summary",
					},
				},
			},
		},
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
	app.SetToolRuntime(toolruntime.Config{ListenAddr: "127.0.0.1:0", StateFile: paths.ToolServiceFile})
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
	var info toolruntime.ServiceInfo
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
	startToolTestRemoteTurn(t, app, "surface-2", "inst-2", "thread-2", "turn-2")

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, toolErr := app.sendIMFileTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
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

func TestSendIMFileToolRejectsMissingCurrentTurnSurface(t *testing.T) {
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
		"path": filePath,
	})
	if toolErr == nil || toolErr.Code != "caller_instance_id_required" {
		t.Fatalf("expected missing caller rejection, got %#v", toolErr)
	}

	_, toolErr = app.sendIMFileTool(withToolCallerInstanceID(context.Background(), "inst-1"), map[string]any{
		"path": filePath,
	})
	if toolErr == nil || toolErr.Code != "current_turn_surface_unavailable" {
		t.Fatalf("expected missing current turn rejection, got %#v", toolErr)
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
			startToolTestRemoteTurn(t, app, "surface-1", "inst-1", "thread-1", "turn-1")

			_, toolErr := app.sendIMFileTool(withToolCallerInstanceID(context.Background(), "inst-1"), map[string]any{
				"path": filePath,
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
	startToolTestRemoteTurn(t, app, "surface-2", "inst-2", "thread-2", "turn-2")

	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	result, toolErr := app.sendIMImageTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
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
	_, toolErr := app.sendIMImageTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"path": textPath,
	})
	if toolErr == nil || toolErr.Code != "invalid_image_path" {
		t.Fatalf("expected invalid image path rejection before surface lookup, got %#v", toolErr)
	}

	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, toolErr = app.sendIMImageTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"path": imagePath,
	})
	if toolErr == nil || toolErr.Code != "current_turn_surface_unavailable" {
		t.Fatalf("expected missing current turn rejection, got %#v", toolErr)
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
			startToolTestRemoteTurn(t, app, "surface-1", "inst-1", "thread-1", "turn-1")

			_, toolErr := app.sendIMImageTool(withToolCallerInstanceID(context.Background(), "inst-1"), map[string]any{
				"path": imagePath,
			})
			if toolErr == nil || toolErr.Code != tc.wantCode {
				t.Fatalf("expected %s, got %#v", tc.wantCode, toolErr)
			}
		})
	}
}

func TestReadDriveFileCommentsToolRoutesByResolvedSurface(t *testing.T) {
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

	result, toolErr := app.readDriveFileCommentsTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"surface_session_id": "surface-1",
		"url":                "https://my.feishu.cn/file/file-token-1",
	})
	if toolErr != nil {
		t.Fatalf("readDriveFileCommentsTool() error = %#v", toolErr)
	}
	if len(sender.commentCalls) != 1 {
		t.Fatalf("expected one comment read call, got %#v", sender.commentCalls)
	}
	if sender.commentCalls[0].GatewayID != "app-2" || sender.commentCalls[0].FileToken != "file-token-1" || sender.commentCalls[0].FileType != "file" {
		t.Fatalf("unexpected routed comment read call: %#v", sender.commentCalls[0])
	}
	typed, ok := result.(driveFileCommentsToolResult)
	if !ok {
		t.Fatalf("unexpected tool result type: %#v", result)
	}
	if typed.SurfaceSessionID != "surface-2" || typed.CommentCount != 1 || len(typed.Comments) != 1 {
		t.Fatalf("unexpected read result: %#v", typed)
	}
}

func TestReadDriveFileCommentsToolRejectsInvalidInputAndDetachedSurface(t *testing.T) {
	sender := &fakeToolSender{}
	app, _ := newToolServiceTestApp(t, sender)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	_, toolErr := app.readDriveFileCommentsTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"url": "https://my.feishu.cn/wiki/wiki-token-1",
	})
	if toolErr == nil || toolErr.Code != "unsupported_document_url" {
		t.Fatalf("expected unsupported document url rejection, got %#v", toolErr)
	}

	_, toolErr = app.readDriveFileCommentsTool(withToolCallerInstanceID(context.Background(), "inst-2"), map[string]any{
		"url": "https://my.feishu.cn/file/file-token-1",
	})
	if toolErr == nil || toolErr.Code != "current_turn_surface_unavailable" {
		t.Fatalf("expected missing current turn rejection, got %#v", toolErr)
	}
}

func TestParseDriveCommentsURL(t *testing.T) {
	testCases := []struct {
		name      string
		rawURL    string
		wantToken string
		wantType  string
		wantCode  string
	}{
		{
			name:      "my feishu file url",
			rawURL:    "https://my.feishu.cn/file/IU5Jb0ZyroW99DxGCvfcJYlenAS",
			wantToken: "IU5Jb0ZyroW99DxGCvfcJYlenAS",
			wantType:  "file",
		},
		{
			name:      "drive file url",
			rawURL:    "https://foo.feishu.cn/drive/file/boxcnABC123",
			wantToken: "boxcnABC123",
			wantType:  "file",
		},
		{
			name:      "docx url with query",
			rawURL:    "https://foo.feishu.cn/docx/doxcnABC123?from=share",
			wantToken: "doxcnABC123",
			wantType:  "docx",
		},
		{
			name:      "sheets url",
			rawURL:    "https://foo.feishu.cn/sheets/shtcnABC123",
			wantToken: "shtcnABC123",
			wantType:  "sheet",
		},
		{
			name:     "wiki unsupported",
			rawURL:   "https://foo.feishu.cn/wiki/wikcnABC123",
			wantCode: "unsupported_document_url",
		},
		{
			name:     "non feishu host unsupported",
			rawURL:   "https://example.com/file/ABC123",
			wantCode: "unsupported_document_url",
		},
		{
			name:     "invalid url",
			rawURL:   "not-a-url",
			wantCode: "invalid_url",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotToken, gotType, toolErr := parseDriveCommentsURL(tc.rawURL)
			if tc.wantCode != "" {
				if toolErr == nil || toolErr.Code != tc.wantCode {
					t.Fatalf("expected error %q, got %#v", tc.wantCode, toolErr)
				}
				return
			}
			if toolErr != nil {
				t.Fatalf("parseDriveCommentsURL() error = %#v", toolErr)
			}
			if gotToken != tc.wantToken || gotType != tc.wantType {
				t.Fatalf("unexpected parse result: token=%q type=%q", gotToken, gotType)
			}
		})
	}
}
