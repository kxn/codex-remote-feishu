package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type toolMCPRequestOptions struct {
	Method           string
	Token            string
	SessionID        string
	ProtocolVersion  string
	CallerInstanceID string
	Body             string
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
	target := "http://127.0.0.1/"
	if strings.TrimSpace(opts.CallerInstanceID) != "" {
		values := url.Values{}
		values.Set(toolCallerInstanceIDQueryParam, strings.TrimSpace(opts.CallerInstanceID))
		target += "?" + values.Encode()
	}
	req := httptest.NewRequest(method, target, strings.NewReader(opts.Body))
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
	return initializeToolMCPSessionWithCaller(t, handler, token, "")
}

func initializeToolMCPSessionWithCaller(t *testing.T, handler http.Handler, token, callerInstanceID string) (string, string) {
	t.Helper()
	rec := performToolMCPRequest(t, handler, toolMCPRequestOptions{
		Method:           http.MethodPost,
		Token:            token,
		CallerInstanceID: callerInstanceID,
		Body:             toolMCPInitializeRequestBody(),
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
		Method:           http.MethodPost,
		Token:            token,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: callerInstanceID,
		Body:             toolMCPNotificationInitializedBody(),
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

func startToolTestRemoteTurn(t *testing.T, app *App, surfaceID, instanceID, threadID, turnID string) {
	t.Helper()
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		MessageID:        "msg-" + surfaceID,
		Text:             "run",
	})
	app.service.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  threadID,
		TurnID:    turnID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID},
	})
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

	startToolTestRemoteTurn(t, app, "surface-normal", "inst-1", "thread-1", "turn-1")
	sessionID, protocolVersion := initializeToolMCPSessionWithCaller(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken, "inst-1")

	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-1",
		Body:             toolMCPCallRequestBody(t, 2, "tools/list", map[string]any{}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	listPayload := decodeToolMCPResponse(t, rec)
	result, _ := listPayload["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("unexpected tools/list payload: %#v", listPayload)
	}
	seen := map[string]map[string]any{}
	for _, raw := range tools {
		if item, ok := raw.(map[string]any); ok {
			seen[strings.TrimSpace(item["name"].(string))] = item
		}
	}
	if _, ok := seen[feishuSurfaceResolverToolName]; ok {
		t.Fatalf("resolver should not be published in MCP tool list: %#v", seen[feishuSurfaceResolverToolName])
	}
	if strings.Contains(seen[feishuSendIMFileToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("file-send description still exposes workspace context: %#v", seen[feishuSendIMFileToolName])
	}
	if strings.Contains(seen[feishuSendIMFileToolName]["description"].(string), "surface_session_id") {
		t.Fatalf("file-send description still exposes surface_session_id: %#v", seen[feishuSendIMFileToolName])
	}
	if strings.Contains(seen[feishuSendIMImageToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("image-send description still exposes workspace context: %#v", seen[feishuSendIMImageToolName])
	}
	if strings.Contains(seen[feishuSendIMVideoToolName]["description"].(string), ".codex-remote/surface-context.json") {
		t.Fatalf("video-send description still exposes workspace context: %#v", seen[feishuSendIMVideoToolName])
	}
	if !strings.Contains(seen[feishuReadDriveFileCommentsToolName]["description"].(string), "Pass the exact Feishu URL") {
		t.Fatalf("drive-comment description missing comment workflow trigger: %#v", seen[feishuReadDriveFileCommentsToolName])
	}
	fileSchema, _ := seen[feishuSendIMFileToolName]["inputSchema"].(map[string]any)
	if strings.Contains(marshalToolPayloadText(fileSchema), "surface_session_id") {
		t.Fatalf("file-send schema should not expose surface_session_id: %#v", fileSchema)
	}

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec = performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-1",
		Body: toolMCPCallRequestBody(t, 4, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"path": filePath,
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
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-1",
		Body: toolMCPCallRequestBody(t, 5, "tools/call", map[string]any{
			"name": feishuSendIMImageToolName,
			"arguments": map[string]any{
				"path": imagePath,
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
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-1",
		Body: toolMCPCallRequestBody(t, 6, "tools/call", map[string]any{
			"name": feishuSendIMVideoToolName,
			"arguments": map[string]any{
				"path": videoPath,
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
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-1",
		Body: toolMCPCallRequestBody(t, 7, "tools/call", map[string]any{
			"name": feishuReadDriveFileCommentsToolName,
			"arguments": map[string]any{
				"url": "https://my.feishu.cn/file/file-token-1",
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

	sessionID, protocolVersion := initializeToolMCPSessionWithCaller(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken, "inst-missing")
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:           http.MethodPost,
		Token:            app.toolRuntime.BearerToken,
		SessionID:        sessionID,
		ProtocolVersion:  protocolVersion,
		CallerInstanceID: "inst-missing",
		Body: toolMCPCallRequestBody(t, 2, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"path": filePath,
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
	if errPayload["code"] != "current_turn_surface_unavailable" {
		t.Fatalf("unexpected tool error payload: %#v", payload)
	}
}

func TestToolMCPCallUsesSessionCallerInstanceFromInitializeRequest(t *testing.T) {
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
		SurfaceSessionID: "surface-current",
		GatewayID:        "app-current",
		ChatID:           "chat-current",
		ActorUserID:      "user-current",
		InstanceID:       "inst-1",
	})
	startToolTestRemoteTurn(t, app, "surface-current", "inst-1", "thread-1", "turn-1")

	sessionID, protocolVersion := initializeToolMCPSessionWithCaller(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken, "inst-1")
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 2, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"path": filePath,
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send file tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.fileCalls) != 1 {
		t.Fatalf("expected one file send call, got %#v", sender.fileCalls)
	}
	if got := sender.fileCalls[0]; got.GatewayID != "app-current" || got.SurfaceSessionID != "surface-current" || got.ChatID != "chat-current" {
		t.Fatalf("tool call did not use session caller surface: %#v", got)
	}
}

func TestToolMCPCallRoutesToCurrentTurnSurfaceAndIgnoresLegacySurfaceArgument(t *testing.T) {
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
	app.service.MaterializeSurfaceResume("surface-single", "app-single", "chat-single", "user-single", state.ProductModeNormal, agentproto.BackendCodex, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	app.service.MaterializeSurfaceResume("surface-group", "app-group", "chat-group", "user-group", state.ProductModeNormal, agentproto.BackendCodex, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	app.service.Surface("surface-single").AttachedInstanceID = "inst-1"
	app.service.Surface("surface-group").AttachedInstanceID = "inst-1"
	startToolTestRemoteTurn(t, app, "surface-group", "inst-1", "thread-1", "turn-1")

	sessionID, protocolVersion := initializeToolMCPSessionWithCaller(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken, "inst-1")
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 2, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"surface_session_id": "surface-single",
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
	if got := sender.fileCalls[0]; got.GatewayID != "app-group" || got.SurfaceSessionID != "surface-group" || got.ChatID != "chat-group" {
		t.Fatalf("legacy surface argument should be ignored in favor of current turn surface: %#v", got)
	}
}

func TestToolMCPCallCanResolvePendingRemoteTurnBeforeTurnStarted(t *testing.T) {
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
		SurfaceSessionID: "surface-pending",
		GatewayID:        "app-pending",
		ChatID:           "chat-pending",
		ActorUserID:      "user-pending",
		InstanceID:       "inst-1",
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-pending",
		MessageID:        "msg-pending",
		Text:             "run",
	})
	if pending := app.service.PendingRemoteTurns(); len(pending) != 1 || pending[0].SurfaceSessionID != "surface-pending" {
		t.Fatalf("expected pending remote turn before MCP call, got %#v", pending)
	}

	sessionID, protocolVersion := initializeToolMCPSessionWithCaller(t, app.toolRuntime.Server.Handler, app.toolRuntime.BearerToken, "inst-1")
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	rec := performToolMCPRequest(t, app.toolRuntime.Server.Handler, toolMCPRequestOptions{
		Method:          http.MethodPost,
		Token:           app.toolRuntime.BearerToken,
		SessionID:       sessionID,
		ProtocolVersion: protocolVersion,
		Body: toolMCPCallRequestBody(t, 2, "tools/call", map[string]any{
			"name": feishuSendIMFileToolName,
			"arguments": map[string]any{
				"path": filePath,
			},
		}),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("send file tool call failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.fileCalls) != 1 {
		t.Fatalf("expected one file send call, got %#v", sender.fileCalls)
	}
	if got := sender.fileCalls[0]; got.GatewayID != "app-pending" || got.SurfaceSessionID != "surface-pending" || got.ChatID != "chat-pending" {
		t.Fatalf("tool call did not route through pending remote turn: %#v", got)
	}
}
