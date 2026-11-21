package wrapper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

// The last inline overlay retains provider/auth/model settings and installs the
// frozen platform instructions in native agent configuration, never user input.
func (a *App) applyWorkOpenCodeEnv(env []string) ([]string, error) {
	if a.work == nil {
		return env, nil
	}
	overlay := map[string]any{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if key == config.OpenCodeConfigContentEnv {
			if json.Unmarshal([]byte(value), &overlay) != nil || overlay == nil { // Existing profile material must not silently disappear.
				return nil, fmt.Errorf("managed OpenCode profile overlay must be a valid JSON object")
			}
		}
	}
	agents, _ := overlay["agent"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
	}
	for _, name := range []string{"build", "plan"} {
		if agents[name] == nil {
			agents[name] = map[string]any{}
		}
	}
	for name, raw := range agents {
		agent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if agent["mode"] == "subagent" {
			continue
		}
		previous, _ := agent["prompt"].(string)
		agent["prompt"] = strings.TrimSpace(previous + "\n\n" + a.work.snapshot.InstructionBundle.Text)
		agents[name] = agent
	}
	if a.work.snapshot.ReadOnly {
		enforceOpenCodeReviewPolicy(overlay, agents)
	}
	overlay["agent"] = agents
	data, _ := json.Marshal(overlay)
	return upsertEnvValue(env, config.OpenCodeConfigContentEnv, string(data)), nil
}
