// Package singleturn 提供与具体协议无关的单次推理调用：
// 请求 = 模型名 + 消息列表（文本/图片），返回纯文本。
//
// 设计上不绑定任何业务语义（视觉、翻译、摘要等都可复用），
// 不引入对话状态，也不依赖 MCP / 飞书 / daemon 结构。
package singleturn

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Protocol 标识底层协议。
type Protocol string

const (
	ProtocolOpenAIChat      Protocol = "openai_chat"
	ProtocolOpenAIResponses Protocol = "openai_responses"
	ProtocolAnthropic       Protocol = "anthropic"
	ProtocolGemini          Protocol = "gemini"
)

// Config 是 Provider 的端点配置。APIKey 为空时请求不带鉴权头。
type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	MaxTokens int
}

// Provider 是单次推理接口。
type Provider interface {
	Complete(ctx context.Context, req Request) (string, error)
}

// Request 是一次单次推理请求。
type Request struct {
	Model    string
	Messages []Message
}

// Message 是一条用户消息，可包含文本和/或图片。
type Message struct {
	Text   string
	Images []Image
}

// Image 是图片消息；Data 为原始字节，MIMEType 为媒体类型。
// ID 可选，多图时用于在 prompt 中引用（adapter 不负责 id 映射）。
type Image struct {
	ID       string
	Data     []byte
	MIMEType string
}

// NewProvider 按协议构造 Provider。
func NewProvider(protocol Protocol, cfg Config) (Provider, error) {
	cfg.BaseURL = trimURL(cfg.BaseURL)
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("singleturn: base url is required")
	}
	switch protocol {
	case ProtocolOpenAIChat:
		return &openAIChatProvider{cfg: cfg, client: defaultHTTPClient()}, nil
	case ProtocolOpenAIResponses:
		return &openAIResponsesProvider{cfg: cfg, client: defaultHTTPClient()}, nil
	case ProtocolAnthropic:
		return &anthropicProvider{cfg: cfg, client: defaultHTTPClient()}, nil
	case ProtocolGemini:
		return &geminiProvider{cfg: cfg, client: defaultHTTPClient()}, nil
	default:
		return nil, fmt.Errorf("singleturn: unsupported protocol %q", protocol)
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

func modelOrDefault(cfg Config, model string) string {
	if model = trim(model); model != "" {
		return model
	}
	return trim(cfg.Model)
}
