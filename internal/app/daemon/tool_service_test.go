package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func newToolServiceTestApp(t *testing.T) (*App, relayruntime.Paths) {
	t.Helper()
	stateDir := t.TempDir()
	paths := relayruntime.Paths{StateDir: stateDir, ToolServiceFile: filepath.Join(stateDir, "tool-service.json")}
	app := New("127.0.0.1:0", "127.0.0.1:0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: paths})
	app.SetToolRuntime(ToolRuntimeConfig{ListenAddr: "127.0.0.1:0", StateFile: paths.ToolServiceFile})
	return app, paths
}

func TestToolManifestRequiresBearerAndPublishesDescription(t *testing.T) {
	app, paths := newToolServiceTestApp(t)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	req := httptest.NewRequest(http.MethodGet, "/v1/tools/manifest", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.toolServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without bearer, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/tools/manifest", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+app.toolBearerToken)
	rec = httptest.NewRecorder()
	app.toolServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected manifest success, got %d body=%s", rec.Code, rec.Body.String())
	}
	var manifest toolManifestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != feishuSurfaceResolverToolName {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	if !strings.Contains(manifest.Tools[0].Description, ".codex-remote/surface-context.json") {
		t.Fatalf("expected workspace context rule in description, got %q", manifest.Tools[0].Description)
	}
	infoRaw, err := os.ReadFile(paths.ToolServiceFile)
	if err != nil {
		t.Fatalf("read tool service file: %v", err)
	}
	var info toolServiceInfo
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		t.Fatalf("unmarshal tool service file: %v", err)
	}
	if info.Token != app.toolBearerToken || !strings.Contains(info.ManifestURL, "/v1/tools/manifest") {
		t.Fatalf("unexpected tool service file: %#v", info)
	}
}

func TestResolveSurfaceContextToolRejectsVSCodeModeAndResolvesNormalMode(t *testing.T) {
	app, _ := newToolServiceTestApp(t)
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
	if result["workspace_root"] != workspaceRoot || result["product_mode"] != "normal" {
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
