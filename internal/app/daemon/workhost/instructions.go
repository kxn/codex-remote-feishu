package workhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func composeLaunchInstructions(settings LaunchSettings, platform agentproto.InstructionBundle) agentproto.InstructionBundle {
	profile := ""
	switch settings.Backend {
	case work.BackendCodex:
		if settings.CodexResume != nil {
			profile = settings.CodexResume.DeveloperInstruction
		}
	case work.BackendClaude:
		for _, entry := range settings.Environment {
			key, value, _ := strings.Cut(entry, "=")
			if key == config.ClaudeAppendSystemPromptEnv {
				profile = value
			}
		}
	case work.BackendOpenCode:
		for _, entry := range settings.Environment {
			key, value, _ := strings.Cut(entry, "=")
			if key == config.OpenCodeConfigContentEnv {
				var overlay struct {
					Instructions string `json:"instructions"`
				}
				if json.Unmarshal([]byte(value), &overlay) == nil {
					profile = overlay.Instructions
				}
			}
		}
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return platform
	}
	text := profile + "\n\n" + platform.Text
	hash := sha256.Sum256([]byte(text))
	return agentproto.InstructionBundle{Revision: hex.EncodeToString(hash[:]), Text: text}
}

func sameRuntimePolicy(record LaunchRecord, settings LaunchSettings) bool {
	return record.Snapshot.NativeBinaryPath == settings.NativeBinaryPath && record.Snapshot.NativeBinaryVersion == settings.NativeBinaryVersion && record.Snapshot.ReadOnly == settings.ReadOnly && record.PreferencesRevision == settings.PreferencesRevision && reflect.DeepEqual(record.Overrides, settings.Overrides) && reflect.DeepEqual(record.CodexResume, settings.CodexResume)
}
