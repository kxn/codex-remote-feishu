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

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type toolMCPRequestOptions struct {
	Method          string
	Token           string
	SessionID       string
	ProtocolVersion string
	Body            string
}

func toolMCPInitializeRequestBody() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"tool-test","version":"1.0"}}}`
}

func toolMCPNotificationInitializedBody() string {
	return `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
}

func toolMCPCallRequestBody(t *testing.T, id int, method string, params map[string]any) string {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return string(raw)
}

func performToolMCPRequest(t *testing.T, handler http.Handler, opts toolMCPRequestOptions) *httptest.ResponseRecorder {
	t.Helper()
	method := opts.Method
	if method == "" {
		method = http.MethodPost
	}
	req := httptest.NewRequest(method, "http://127.0.0.1/", strings.NewReader(opts.Body))
	req.Host = "127.0.0.1:9502"
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Accept", "application/json, text/event-stream")
	if opts.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+opts.Token)
	}
	if opts.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", opts.SessionID)
	}
	if opts.ProtocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", opts.ProtocolVersion)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func initializeToolMCPSession(t *testing.T, handler http.Handler, token string) (string, string) {
	t.Helper()
	rec := performToolMCPRequest(t, handler, toolMCPRequestOptions{
		Method: http.MethodPost,
		Token:  token,
		Body:   toolMCPInitializeRequestBody(),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	sessionID := strings.TrimSpace(rec.Header().Get("Mcp-Session-Id"))
	if sessionID == "" {
		t.Fatalf("initialize missing session id header: headers=%v", rec.Header())
	}
	var payload struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal initialize response: %v", err)
	}
	protocolVersion := strings.TrimSpace(payload.Result.ProtocolVersion)
	if protocolVersion == "" {
		t.Fatalf("initialize missing protocol version: body=%s", rec.Body.String())
	}
	rec = performToolMCPRequest(t, handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           token,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body:            toolMCPNotificationInitializedBody(),
	})
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("initialized notification failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	return sessionID, protocolVersion
}

func decodeToolMCPResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, rec.Body.String())
	}
	return payload
}

func TestToolMCPListAndCallTools(t *testing.T) {
	sender := &fakeToolSender{}
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
		SurfaceSessionID: "surface-normal",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	sessionID, protocolVersion := initializeToolMCPSession(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken)

	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body:            toolMCPCallRequestBody(t, 2, "tools/list", map[string]any{}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	listPayload := decodeToolMCPResponse(t, rec)
	result, _ := listPayload["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("unexpected tools/list payload: %#v", listPayload)
	}
	seen := map[string]map[string]any{}
	for _, raw := range tools {
		if item, ok := raw.(map[string]any); ok {
			seen[strings.TrimSpace(item["name"].(string))] = item
		}
	}
	if !strings.Contains(seen[feishuSurfaceResolverToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("resolver description missing workspace context rule: %#v", seen[feishuSurfaceResolverToolName])
	}
	if !strings.Contains(seen[feishuSendIMFileToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("file-send description missing workspace context rule: %#v", seen[feishuSendIMFileToolName])
	}
	if !strings.Contains(seen[feishuSendIMImageToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("image-send description missing workspace context rule: %#v", seen[feishuSendIMImageToolName])
	}
	if !strings.Contains(seen[feishuSendIMVideoToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("video-send description missing workspace context rule: %#v", seen[feishuSendIMVideoToolName])
	}
	if !strings.Contains(seen[feishuReadDriveFileCommentsToolName]["description"].(string), "Pass the exact Feishu URL") {
		t.Fatalf("drive-comment description missing comment workflow trigger: %#v", seen[feishuReadDriveFileCommentsToolName])
	}

	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 3, "tools/call", map[string]any{
			"name": feishuSurfaceResolverToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-normal",
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("resolver tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	resolvePayload := decodeToolMCPResponse(t, rec)
	callResult, _ := resolvePayload["result"].(map[string]any)
	if callResult["isError"] == true {
		t.Fatalf("resolver tool unexpectedly returned error: %#v", resolvePayload)
	}
	structured, _ := callResult["structuredContent"].(map[string]any)
	if structured["surface_session_id"] != "surface-normal" || structured["product_mode"] != "normal" {
		t.Fatalf("unexpected resolver structured content: %#v", structured)
	}

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 4, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-normal",
				"path":               filePath,
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send file tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.fileCalls) != 1 {
		t.Fatalf("expected one file send call, got %#v", sender.fileCalls)
	}
	sendPayload := decodeToolMCPResponse(t, rec)
	sendResult, _ := sendPayload["result"].(map[string]any)
	sendStructured, _ := sendResult["structuredContent"].(map[string]any)
	if sendStructured["surface_session_id"] != "surface-normal" || sendStructured["message_id"] != "msg-file" {
		t.Fatalf("unexpected send result: %#v", sendPayload)
	}

	imagePath := filepath.Join(t.TempDir(), "preview.png")
	if err := os.WriteFile(imagePath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 5, "tools/call", map[string]any{
			"name": feishuSendIMImageToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-normal",
				"path":               imagePath,
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send image tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.imageCalls) != 1 {
		t.Fatalf("expected one image send call, got %#v", sender.imageCalls)
	}
	sendPayload = decodeToolMCPResponse(t, rec)
	sendResult, _ = sendPayload["result"].(map[string]any)
	sendStructured, _ = sendResult["structuredContent"].(map[string]any)
	if sendStructured["surface_session_id"] != "surface-normal" || sendStructured["message_id"] != "msg-image" {
		t.Fatalf("unexpected image send result: %#v", sendPayload)
	}

	videoPath := filepath.Join(t.TempDir(), "demo.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 6, "tools/call", map[string]any{
			"name": feishuSendIMVideoToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-normal",
				"path":               videoPath,
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send video tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.videoCalls) != 1 {
		t.Fatalf("expected one video send call, got %#v", sender.videoCalls)
	}
	sendPayload = decodeToolMCPResponse(t, rec)
	sendResult, _ = sendPayload["result"].(map[string]any)
	sendStructured, _ = sendResult["structuredContent"].(map[string]any)
	if sendStructured["surface_session_id"] != "surface-normal" || sendStructured["message_id"] != "msg-video" {
		t.Fatalf("unexpected video send result: %#v", sendPayload)
	}

	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 7, "tools/call", map[string]any{
			"name": feishuReadDriveFileCommentsToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-normal",
				"url":                "https://my.feishu.cn/file/file-token-1",
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("read comments tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.commentCalls) != 1 {
		t.Fatalf("expected one drive comment read call, got %#v", sender.commentCalls)
	}
	readPayload := decodeToolMCPResponse(t, rec)
	readResult, _ := readPayload["result"].(map[string]any)
	readStructured, _ := readResult["structuredContent"].(map[string]any)
	if readStructured["surface_session_id"] != "surface-normal" || readStructured["comment_count"] != float64(1) {
		t.Fatalf("unexpected read comments result: %#v", readPayload)
	}
}

func TestToolMCPBusinessErrorsStayInToolResult(t *testing.T) {
	app, _ := newToolServiceTestApp(t, nil)
	if err := app.Bind(); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	sessionID, protocolVersion := initializeToolMCPSession(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken)
	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 2, "tools/call", map[string]any{
			"name": feishuSurfaceResolverToolName,
			"arguments": map[string]any{
				"surface_session_id": "missing-surface",
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tool error to stay in result, got code=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeToolMCPResponse(t, rec)
	result, _ := payload["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected isError=true, got %#v", payload)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	errPayload, _ := structured["error"].(map[string]any)
	if errPayload["code"] != "surface_not_found" {
		t.Fatalf("unexpected tool error payload: %#v", payload)
	}
}
