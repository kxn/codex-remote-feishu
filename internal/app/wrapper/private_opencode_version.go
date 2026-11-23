package wrapper

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"strings"
)

// verifyPrivateOpenCodeBootstrap uses the response from the actual spawned ACP
// process. The supported v2 adapter fences session updates on its durable
// inbox-delivery ID and serializes prompts per session. Reject prereleases and
// future major versions until their implementation is independently verified.
func verifyPrivateOpenCodeBootstrap(raw []byte) error {
	var v struct {
		Result struct {
			AgentInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"agentInfo"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &v) != nil || !strings.EqualFold(v.Result.AgentInfo.Name, "opencode") {
		return fmt.Errorf("OpenCode ACP identity unavailable; upgrade to OpenCode 2.1.4 or newer in the 2.x series")
	}
	if !agentproto.SupportedPrivateOpenCodeVersion(v.Result.AgentInfo.Version) {
		return fmt.Errorf("OpenCode %s is unsupported; upgrade to OpenCode 2.1.4 or newer in the 2.x series", v.Result.AgentInfo.Version)
	}
	return nil
}
