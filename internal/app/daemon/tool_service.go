package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const feishuSurfaceResolverToolName = "feishu_resolve_surface_context"
const feishuSendIMFileToolName = "feishu_send_im_file"

const feishuSurfaceResolverDescription = "Resolve the current Feishu remote surface context. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. If the file is missing, invalid, or you are not in normal remote workspace mode, do not guess."
const feishuSendIMFileDescription = "Send a local file to the current Feishu remote surface as an IM file message. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. Use a real local file path and do not guess a surface, chat, or remote URL."

type toolServiceInfo struct {
	URL         string    `json:"url"`
	ManifestURL string    `json:"manifestUrl"`
	CallURL     string    `json:"callUrl"`
	Token       string    `json:"token"`
	TokenType   string    `json:"tokenType"`
	GeneratedAt time.Time `json:"generatedAt"`
}

type toolManifestResponse struct {
	Tools []toolDefinition `json:"tools"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type toolCallResponse struct {
	Result any `json:"result"`
}

type toolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type toolErrorPayload struct {
	Error toolError `json:"error"`
}

type resolvedToolSurfaceContext struct {
	SurfaceSessionID   string
	Platform           string
	GatewayID          string
	ChatID             string
	ActorUserID        string
	ProductMode        string
	AttachedInstanceID string
	SelectedThreadID   string
	RouteMode          string
	WorkspaceKey       string
	WorkspaceRoot      string
	InstanceSource     string
	InstanceManaged    bool
	Attached           bool
}

func (a *App) SetToolRuntime(cfg ToolRuntimeConfig) {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /v1/tools/manifest", a.requireToolAuth(a.handleToolManifest))
	mux.HandleFunc("POST /v1/tools/call", a.requireToolAuth(a.handleToolCall))
	a.toolServer = &http.Server{Addr: cfg.ListenAddr, Handler: mux}
	a.toolStatePath = strings.TrimSpace(cfg.StateFile)
}

func (a *App) bindToolListenerLocked() error {
	if a.toolServer == nil || a.toolListener != nil {
		return nil
	}
	listener, err := net.Listen("tcp", a.toolServer.Addr)
	if err != nil {
		return err
	}
	token, err := generateToolBearerToken()
	if err != nil {
		_ = listener.Close()
		return err
	}
	a.toolListener = listener
	a.toolBearerToken = token
	if err := a.persistToolServiceStateLocked(); err != nil {
		_ = listener.Close()
		a.toolListener = nil
		a.toolBearerToken = ""
		return err
	}
	return nil
}

func generateToolBearerToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (a *App) persistToolServiceStateLocked() error {
	if strings.TrimSpace(a.toolStatePath) == "" || a.toolListener == nil || strings.TrimSpace(a.toolBearerToken) == "" {
		return nil
	}
	info := toolServiceInfo{
		URL:         "http://" + a.toolListener.Addr().String(),
		ManifestURL: "http://" + a.toolListener.Addr().String() + "/v1/tools/manifest",
		CallURL:     "http://" + a.toolListener.Addr().String() + "/v1/tools/call",
		Token:       a.toolBearerToken,
		TokenType:   "bearer",
		GeneratedAt: time.Now().UTC(),
	}
	return writeJSONFileAtomic(a.toolStatePath, info, 0o600)
}

func (a *App) removeToolServiceStateLocked() {
	if strings.TrimSpace(a.toolStatePath) == "" {
		return
	}
	if err := os.Remove(a.toolStatePath); err != nil && !os.IsNotExist(err) {
		log.Printf("remove tool service state failed: path=%s err=%v", a.toolStatePath, err)
	}
}

func (a *App) requireToolAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !adminauth.IsLoopbackRequest(r) {
			writeToolError(w, http.StatusForbidden, toolError{
				Code:    "loopback_required",
				Message: "tool service only accepts loopback requests",
			})
			return
		}
		expected := strings.TrimSpace(a.toolBearerToken)
		if expected == "" {
			writeToolError(w, http.StatusServiceUnavailable, toolError{
				Code:    "tool_service_not_ready",
				Message: "tool service is not ready",
			})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			writeToolError(w, http.StatusUnauthorized, toolError{
				Code:    "invalid_token",
				Message: "missing or invalid bearer token",
			})
			return
		}
		next(w, r)
	}
}

func (a *App) handleToolManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, toolManifestResponse{
		Tools: []toolDefinition{
			{
				Name:        feishuSurfaceResolverToolName,
				Description: feishuSurfaceResolverDescription,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"surface_session_id": map[string]any{
							"type":        "string",
							"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
						},
					},
					"required":             []string{"surface_session_id"},
					"additionalProperties": false,
				},
			},
			{
				Name:        feishuSendIMFileToolName,
				Description: feishuSendIMFileDescription,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"surface_session_id": map[string]any{
							"type":        "string",
							"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
						},
						"path": map[string]any{
							"type":        "string",
							"description": "Existing local file path to send as a Feishu IM file message",
						},
					},
					"required":             []string{"surface_session_id", "path"},
					"additionalProperties": false,
				},
			},
		},
	})
}

func (a *App) handleToolCall(w http.ResponseWriter, r *http.Request) {
	var req toolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeToolError(w, http.StatusBadRequest, toolError{
			Code:    "invalid_request",
			Message: "invalid tool call payload",
		})
		return
	}
	switch strings.TrimSpace(req.Tool) {
	case feishuSurfaceResolverToolName:
		result, apiErr := a.resolveSurfaceContextTool(req.Arguments)
		if apiErr != nil {
			writeToolError(w, http.StatusBadRequest, *apiErr)
			return
		}
		writeJSON(w, http.StatusOK, toolCallResponse{Result: result})
	case feishuSendIMFileToolName:
		result, apiErr := a.sendIMFileTool(r.Context(), req.Arguments)
		if apiErr != nil {
			writeToolError(w, http.StatusBadRequest, *apiErr)
			return
		}
		writeJSON(w, http.StatusOK, toolCallResponse{Result: result})
	default:
		writeToolError(w, http.StatusNotFound, toolError{
			Code:    "tool_not_found",
			Message: "unknown tool",
		})
	}
}

func (a *App) resolveSurfaceContextTool(arguments map[string]any) (map[string]any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	a.mu.Lock()
	resolved, apiErr := a.resolveToolSurfaceContextLocked(surfaceID)
	a.mu.Unlock()
	if apiErr != nil {
		return nil, apiErr
	}
	result := map[string]any{
		"surface_session_id":   resolved.SurfaceSessionID,
		"platform":             resolved.Platform,
		"gateway_id":           resolved.GatewayID,
		"chat_id":              resolved.ChatID,
		"actor_user_id":        resolved.ActorUserID,
		"product_mode":         resolved.ProductMode,
		"attached_instance_id": resolved.AttachedInstanceID,
		"selected_thread_id":   resolved.SelectedThreadID,
		"route_mode":           resolved.RouteMode,
	}
	if resolved.WorkspaceKey != "" {
		result["workspace_key"] = resolved.WorkspaceKey
	}
	if resolved.WorkspaceRoot != "" {
		result["workspace_root"] = resolved.WorkspaceRoot
		result["instance_source"] = resolved.InstanceSource
		result["instance_managed"] = resolved.InstanceManaged
	}
	log.Printf("tool call: tool=%s surface=%s status=ok", feishuSurfaceResolverToolName, surfaceID)
	return result, nil
}

func (a *App) sendIMFileTool(ctx context.Context, arguments map[string]any) (map[string]any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	path, _ := arguments["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, &toolError{
			Code:    "path_required",
			Message: "path is required",
		}
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, &toolError{
			Code:    "file_not_found",
			Message: "path does not exist",
		}
	case err != nil:
		return nil, &toolError{
			Code:    "file_access_failed",
			Message: "failed to access local file",
		}
	case info.IsDir():
		return nil, &toolError{
			Code:    "invalid_file_path",
			Message: "path must point to a file",
		}
	}

	a.mu.Lock()
	resolved, apiErr := a.resolveToolSurfaceContextLocked(surfaceID)
	a.mu.Unlock()
	if apiErr != nil {
		return nil, apiErr
	}
	if !resolved.Attached {
		return nil, &toolError{
			Code:    "surface_not_attached",
			Message: "surface is not attached to a workspace",
		}
	}

	sender, ok := a.gateway.(feishu.IMFileSender)
	if !ok {
		return nil, &toolError{
			Code:    "tool_unavailable",
			Message: "Feishu IM file sending is not available in this runtime",
		}
	}
	result, err := sender.SendIMFile(ctx, feishu.IMFileSendRequest{
		GatewayID:        resolved.GatewayID,
		SurfaceSessionID: resolved.SurfaceSessionID,
		ChatID:           resolved.ChatID,
		ActorUserID:      resolved.ActorUserID,
		Path:             path,
	})
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var sendErr *feishu.IMFileSendError
		if errors.As(err, &sendErr) {
			switch sendErr.Code {
			case feishu.IMFileSendErrorUploadFailed:
				return nil, &toolError{Code: "upload_failed", Message: sendErr.Error()}
			case feishu.IMFileSendErrorSendFailed, feishu.IMFileSendErrorMissingReceiveTarget:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			case feishu.IMFileSendErrorGatewayNotRunning:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			}
		}
		return nil, &toolError{
			Code:      "send_failed",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	log.Printf("tool call: tool=%s surface=%s path=%s status=ok message=%s", feishuSendIMFileToolName, resolved.SurfaceSessionID, path, result.MessageID)
	return map[string]any{
		"surface_session_id": result.SurfaceSessionID,
		"gateway_id":         result.GatewayID,
		"file_name":          result.FileName,
		"file_key":           result.FileKey,
		"message_id":         result.MessageID,
	}, nil
}

func (a *App) resolveToolSurfaceContextLocked(surfaceID string) (resolvedToolSurfaceContext, *toolError) {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_session_id_required",
			Message: "surface_session_id is required",
		}
	}

	var surfaceRecord *state.SurfaceConsoleRecord
	for _, current := range a.service.Surfaces() {
		if current != nil && strings.TrimSpace(current.SurfaceSessionID) == surfaceID {
			surfaceRecord = current
			break
		}
	}
	if surfaceRecord == nil {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_not_found",
			Message: "surface_session_id does not exist",
		}
	}
	if state.NormalizeProductMode(surfaceRecord.ProductMode) != state.ProductModeNormal {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_mode_unsupported",
			Message: "Feishu MCP tools are only available in normal mode",
		}
	}

	resolved := resolvedToolSurfaceContext{
		SurfaceSessionID:   surfaceID,
		Platform:           strings.TrimSpace(surfaceRecord.Platform),
		GatewayID:          strings.TrimSpace(surfaceRecord.GatewayID),
		ChatID:             strings.TrimSpace(surfaceRecord.ChatID),
		ActorUserID:        strings.TrimSpace(surfaceRecord.ActorUserID),
		ProductMode:        string(state.NormalizeProductMode(surfaceRecord.ProductMode)),
		AttachedInstanceID: strings.TrimSpace(surfaceRecord.AttachedInstanceID),
		SelectedThreadID:   strings.TrimSpace(surfaceRecord.SelectedThreadID),
		RouteMode:          strings.TrimSpace(string(surfaceRecord.RouteMode)),
	}
	if snapshot := a.service.SurfaceSnapshot(surfaceID); snapshot != nil {
		resolved.WorkspaceKey = strings.TrimSpace(snapshot.WorkspaceKey)
	}
	if inst := a.service.Instance(resolved.AttachedInstanceID); inst != nil {
		resolved.Attached = true
		resolved.WorkspaceRoot = strings.TrimSpace(inst.WorkspaceRoot)
		resolved.InstanceSource = strings.TrimSpace(inst.Source)
		resolved.InstanceManaged = inst.Managed
	}
	return resolved, nil
}

func writeToolError(w http.ResponseWriter, status int, apiErr toolError) {
	writeJSON(w, status, toolErrorPayload{Error: apiErr})
}

func writeJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
