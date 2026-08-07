// Package toolservicecontract 定义 daemon 与子进程（wrapper / codex）之间的
// Feishu MCP tool service 握手协议：
//
//   - ServiceInfo：tool service 状态文件（manifest）schema，daemon 写入、
//     wrapper 读取；
//   - Error / ErrorPayload：MCP 工具调用错误信封；
//   - CallerInstanceIDQueryParam：MCP 工具调用 URL 上标识当前 remote turn
//     实例的查询参数名（daemon 校验、wrapper 追加，同一值）。
//
// 单一事实来源：协议改任何一处必须同步 daemon 与 wrapper，双方只允许引用
// 本包，禁止在调用点内联 URL / token / 状态文件字段字面量。
package toolservicecontract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CallerInstanceIDQueryParam 是 MCP 工具调用 URL 上标识调用方 remote turn
// 实例的查询参数名。daemon（requireToolAuth）校验它，wrapper
// （appendToolCallerInstanceParam）追加它，两端必须使用同一值。
const CallerInstanceIDQueryParam = "codex_remote_instance_id"

// ServiceInfo 是 tool service 状态文件的 schema。daemon 在
// internal/app/daemon/toolruntime 写入；wrapper 在
// internal/app/wrapper/app_child_mcp.go 读取以发布 MCP 服务给子进程。
type ServiceInfo struct {
	URL         string    `json:"url"`
	Protocol    string    `json:"protocol,omitempty"`
	Transport   string    `json:"transport,omitempty"`
	ManifestURL string    `json:"manifestUrl,omitempty"`
	CallURL     string    `json:"callUrl,omitempty"`
	Token       string    `json:"token"`
	TokenType   string    `json:"tokenType"`
	GeneratedAt time.Time `json:"generatedAt"`
}

// Error 是 MCP 工具调用错误信封（toolError）。daemon 侧通过类型别名
// toolError 使用，序列化格式为 {"error": {...}}。
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

// ErrorPayload 包装 Error，是错误信封的完整 JSON 形状。
type ErrorPayload struct {
	Error Error `json:"error"`
}

// ReadServiceInfo 读取并解析 tool service 状态文件。只校验必要字段：
// URL 与 Token 由调用方按需检查（wrapper 要求两者非空）。
func ReadServiceInfo(path string) (ServiceInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ServiceInfo{}, fmt.Errorf("tool service state path is empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ServiceInfo{}, err
	}
	var info ServiceInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return ServiceInfo{}, err
	}
	return info, nil
}

// WriteJSONFileAtomic 以原子方式（临时文件 + rename）写 JSON 文件。
// path 为空是编程错误，返回明确错误而不是静默跳过。
func WriteJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("json file path is empty")
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
