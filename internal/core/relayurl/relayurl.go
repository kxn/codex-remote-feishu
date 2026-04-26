package relayurl

import (
	"net/url"
)

// NormalizeAgentURL normalizes relay websocket URLs for Codex agent traffic.
// When the URL path is empty or root, it fills `/ws/agent`.
func NormalizeAgentURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws/agent"
	}
	return parsed.String()
}
