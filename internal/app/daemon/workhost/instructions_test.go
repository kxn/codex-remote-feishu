package workhost

import (
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
)

func TestLaunchBundleKeepsDefaultNativeProfileInstruction(t *testing.T) {
	platform := agentproto.InstructionBundle{Revision: "platform-v1", Text: "scoped platform instruction"}
	for _, settings := range []LaunchSettings{
		{Backend: work.BackendClaude, Environment: []string{config.ClaudeAppendSystemPromptEnv + "=default native profile", "API_KEY=never-in-prompt"}},
		{Backend: work.BackendCodex, CodexResume: &agentproto.CodexResumePolicy{DeveloperInstruction: "default native profile"}},
		{Backend: work.BackendOpenCode, Environment: []string{config.OpenCodeConfigContentEnv + `={"instructions":"default native profile","provider":{"key":"never-in-prompt"}}`}},
	} {
		result := composeLaunchInstructions(settings, platform)
		if result.Text != "default native profile\n\nscoped platform instruction" || result.Revision == platform.Revision || strings.Contains(result.Text, "never-in-prompt") {
			t.Fatalf("profile composition failed for %s", settings.Backend)
		}
		if composeLaunchInstructions(settings, platform) != result {
			t.Fatal("launch instruction revision was not deterministic")
		}
	}
}
