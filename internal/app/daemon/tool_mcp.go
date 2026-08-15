package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolMCPServerName = "codex-remote-feishu-tool-service"

const toolMCPSessionTimeout = 30 * time.Minute

type toolCallerInstanceIDContextKey struct{}

func (a *App) newToolRuntimeHandler() http.Handler {
	handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return a.newToolMCPServerForRequest(req)
	}, &mcp.StreamableHTTPOptions{
		JSONResponse:   true,
		SessionTimeout: toolMCPSessionTimeout,
	})
	return a.requireToolAuth(handler)
}

// newToolMCPServerForRequest 按调用方（caller instance）当前生效 profile 决定
// 是否注册 describe_image：profile 声明主模型支持直接看图时不注册。
func (a *App) newToolMCPServerForRequest(req *http.Request) *mcp.Server {
	server := a.newToolMCPServer()
	// 只有同时满足“端点已配置”和“主模型未声明支持看图”时才注入工具：
	// 未配置端点时注入一个必失败的工具体验更糟。
	if req == nil || a.callerProfileVisionSupported(req) || !a.visionAssistEndpointConfigured() {
		return server
	}
	for _, definition := range describeImageToolDefinitions() {
		definition := definition
		server.AddTool(&mcp.Tool{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		}, func(ctx context.Context, callReq *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return a.handleMCPToolCall(ctx, definition.Name, callReq)
		})
	}
	return server
}

func withToolCallerInstanceID(ctx context.Context, instanceID string) context.Context {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallerInstanceIDContextKey{}, instanceID)
}

func toolCallerInstanceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(toolCallerInstanceIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

func (a *App) newToolMCPServer() *mcp.Server {
	version := strings.TrimSpace(a.serverIdentity.Version)
	if version == "" {
		version = "dev"
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    toolMCPServerName,
		Version: version,
	}, nil)
	for _, definition := range toolDefinitions() {
		definition := definition
		server.AddTool(&mcp.Tool{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return a.handleMCPToolCall(ctx, definition.Name, req)
		})
	}
	return server
}

func (a *App) handleMCPToolCall(ctx context.Context, toolName string, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	arguments, err := decodeMCPToolArguments(req)
	if err != nil {
		return nil, err
	}

	var (
		result any
		apiErr *toolError
	)
	switch strings.TrimSpace(toolName) {
	case describeImageToolName:
		result, apiErr = a.describeImageTool(ctx, arguments)
	case feishuSendIMFileToolName:
		result, apiErr = a.sendIMFileTool(ctx, arguments)
	case feishuSendIMImageToolName:
		result, apiErr = a.sendIMImageTool(ctx, arguments)
	case feishuSendIMVideoToolName:
		result, apiErr = a.sendIMVideoTool(ctx, arguments)
	case feishuReadDriveFileCommentsToolName:
		result, apiErr = a.readDriveFileCommentsTool(ctx, arguments)
	default:
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeMethodNotFound,
			Message: "unknown tool",
		}
	}
	if apiErr != nil {
		log.Printf("tool call: tool=%s status=error code=%s message=%s", toolName, apiErr.Code, apiErr.Message)
		return newMCPToolErrorResult(*apiErr), nil
	}
	return newMCPToolResult(result), nil
}

func decodeMCPToolArguments(req *mcp.CallToolRequest) (map[string]any, error) {
	if len(req.Params.Arguments) == 0 {
		return map[string]any{}, nil
	}
	var arguments map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &arguments); err != nil {
		return nil, &jsonrpc.Error{
			Code:    jsonrpc.CodeInvalidParams,
			Message: "invalid tool arguments",
		}
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return arguments, nil
}

func newMCPToolResult(payload any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: marshalToolPayloadText(payload)}},
		StructuredContent: payload,
	}
}

func newMCPToolErrorResult(apiErr toolError) *mcp.CallToolResult {
	payload := toolErrorPayload{Error: apiErr}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: marshalToolPayloadText(payload)}},
		StructuredContent: payload,
		IsError:           true,
	}
}

func marshalToolPayloadText(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("marshal tool payload failed: err=%v", err)
		return "{}"
	}
	return string(raw)
}
