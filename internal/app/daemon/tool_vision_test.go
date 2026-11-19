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

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func visionAssistTestApp(t *testing.T, cfg config.AppConfig) (*App, string) {
	t.Helper()
	return newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
}

func registerVisionTestInstance(app *App, backend agentproto.Backend, profileID string) {
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-vision-1",
		Backend:           backend,
		CodexProfileID:    profileID,
		ClaudeProfileID:   profileID,
		OpenCodeProfileID: profileID,
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads:           map[string]*state.ThreadRecord{},
	})
}

func writePNGForTest(t *testing.T, path string) {
	t.Helper()
	data := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
}

func visionArguments(imagePath, prompt string) map[string]any {
	images := []any{map[string]any{"id": "img1", "image": imagePath}}
	if prompt == "" {
		return map[string]any{"images": images}
	}
	return map[string]any{"images": images, "prompt": prompt}
}

func visionToolContext() context.Context {
	return withToolCallerInstanceID(context.Background(), "inst-vision-1")
}

func TestDescribeImageToolSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q, want Bearer test-key", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
			t.Fatalf("unexpected image url %q", imageURL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"图里有一个报错弹窗"}}]}`))
	}))
	defer server.Close()

	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{
		Protocol: "openai_chat",
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "vision-model",
	}
	app, _ := visionAssistTestApp(t, cfg)
	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_native")

	imagePath := filepath.Join(t.TempDir(), "shot.png")
	writePNGForTest(t, imagePath)

	result, toolErr := app.describeImageTool(visionToolContext(), visionArguments(imagePath, "报错是什么"))
	if toolErr != nil {
		t.Fatalf("describe image: %v", toolErr)
	}
	text, ok := result.(map[string]any)["text"].(string)
	if !ok || text != "图里有一个报错弹窗" {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestDescribeImageToolRejectsWhenProfileSupportsVision(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://unused"}
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{{
		ID:              "cp_v2",
		CurrentRevision: 1,
		Revisions: []config.CodexAPIProfileSecretConfig{{
			ID: "cp_v2", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
			Kind: state.CodexProfileKindAPI, Name: "Vision", BaseURL: "http://unused", APIKey: "k",
			Model: "gpt-v", VisionSupported: true,
		}},
	}}
	app, _ := visionAssistTestApp(t, cfg)
	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_v2")

	imagePath := filepath.Join(t.TempDir(), "shot.png")
	writePNGForTest(t, imagePath)
	_, toolErr := app.describeImageTool(visionToolContext(), visionArguments(imagePath, ""))
	if toolErr == nil || toolErr.Code != "describe_image_not_needed" {
		t.Fatalf("expected describe_image_not_needed, got %#v", toolErr)
	}
}

func TestDescribeImageToolFallsBackToMinimalPrompt(t *testing.T) {
	var capturedText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		messages := payload["messages"].([]any)
		content := messages[0].(map[string]any)["content"].([]any)
		capturedText = content[0].(map[string]any)["text"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{
		Protocol: "openai_chat",
		BaseURL:  server.URL,
		Model:    "vision-model",
	}
	app, _ := visionAssistTestApp(t, cfg)
	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_native")

	imagePath := filepath.Join(t.TempDir(), "shot.png")
	writePNGForTest(t, imagePath)
	if _, toolErr := app.describeImageTool(visionToolContext(), visionArguments(imagePath, "")); toolErr != nil {
		t.Fatalf("describe image: %v", toolErr)
	}
	if !strings.Contains(capturedText, "请描述这张图片") {
		t.Fatalf("expected fallback prompt, got %q", capturedText)
	}
}

func TestDescribeImageToolRejectsUnconfigured(t *testing.T) {
	app, _ := visionAssistTestApp(t, config.DefaultAppConfig())
	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_native")
	imagePath := filepath.Join(t.TempDir(), "shot.png")
	writePNGForTest(t, imagePath)
	_, toolErr := app.describeImageTool(visionToolContext(), visionArguments(imagePath, ""))
	if toolErr == nil || toolErr.Code != "vision_assist_not_configured" {
		t.Fatalf("expected vision_assist_not_configured, got %#v", toolErr)
	}
}

func TestDescribeImageToolRejectsNonImage(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.VisionAssist = config.VisionAssistSettings{Protocol: "openai_chat", BaseURL: "http://unused"}
	app, _ := visionAssistTestApp(t, cfg)
	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_native")

	textPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(textPath, []byte("not an image"), 0o600); err != nil {
		t.Fatalf("write text: %v", err)
	}
	_, toolErr := app.describeImageTool(visionToolContext(), visionArguments(textPath, ""))
	if toolErr == nil || toolErr.Code != "image_not_supported" {
		t.Fatalf("expected image_not_supported, got %#v", toolErr)
	}
}

func TestCallerProfileVisionSupported(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{{
		ID:              "op_v2",
		CurrentRevision: 1,
		Revisions: []config.OpenCodeAPIProfileSecretConfig{{
			ID: "op_v2", Revision: 1, CredentialGeneration: 1, ConnectionGeneration: 1,
			Name: "Vision", BaseURL: "http://unused", APIKey: "k", Model: "m",
			ProviderType: "openai_compatible_chat", VisionSupported: true,
		}},
	}}
	app, _ := visionAssistTestApp(t, cfg)
	registerVisionTestInstance(app, agentproto.BackendOpenCode, "op_v2")
	if !app.callerInstanceProfileVisionSupported("inst-vision-1") {
		t.Fatal("expected opencode profile with vision support to be detected")
	}

	registerVisionTestInstance(app, agentproto.BackendCodex, state.OAuthCodexProfileID)
	if !app.callerInstanceProfileVisionSupported("inst-vision-1") {
		t.Fatal("expected codex oauth profile to be treated as vision-capable")
	}

	registerVisionTestInstance(app, agentproto.BackendCodex, "cp_native")
	if app.callerInstanceProfileVisionSupported("inst-vision-1") {
		t.Fatal("expected native codex profile to default to no vision support")
	}

	if app.callerInstanceProfileVisionSupported("inst-missing") {
		t.Fatal("expected unknown instance to default to no vision support")
	}
}
