package relayurl

import (
	"net/url"
)

// AgentPath 是 relay WebSocket agent 流量的统一路径后缀。
// config 层构造默认 relay URL 与本包路径填充必须引用本常量，
// 禁止在调用点硬编码 "/ws/agent"。
const AgentPath = "/ws/agent"

// NormalizeAgentURL normalizes relay websocket URLs for Codex agent traffic.
// When the URL path is empty or root, it fills `/ws/agent`.
func NormalizeAgentURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = AgentPath
	}
	return parsed.String()
}
