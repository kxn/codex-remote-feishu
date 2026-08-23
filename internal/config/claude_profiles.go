package config

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	ClaudeDefaultProfileID   = "default"
	ClaudeDefaultProfileName = "默认"

	ClaudeAuthModeInherit   = "inherit"
	ClaudeAuthModeAuthToken = "auth_token"

	ClaudeBinaryEnv              = "CLAUDE_BIN"
	ClaudeConfigDirEnv           = "CLAUDE_CONFIG_DIR"
	ClaudeBaseURLEnv             = "ANTHROPIC_BASE_URL"
	ClaudeAuthTokenEnv           = "ANTHROPIC_AUTH_TOKEN"
	ClaudeModelEnv               = "ANTHROPIC_MODEL"
	ClaudeDefaultHaikuModelEnv   = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
	ClaudeSubagentModelEnv       = "CLAUDE_CODE_SUBAGENT_MODEL"
	ClaudeEffortLevelEnv         = "CLAUDE_CODE_EFFORT_LEVEL"
	ClaudeDisableAdaptiveEnv     = "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING"
	ClaudeDisableThinkingEnv     = "CLAUDE_CODE_DISABLE_THINKING"
	ClaudeRuntimeProfileIDEnv    = "CODEX_REMOTE_CLAUDE_PROFILE_ID"
	ClaudeRuntimeSettingsJSONEnv = "CODEX_REMOTE_CLAUDE_SETTINGS_JSON"
	ClaudeAppendSystemPromptEnv  = "CODEX_REMOTE_CLAUDE_APPEND_SYSTEM_PROMPT"
)

var claudeProfileLaunchEnvKeys = []string{
	ClaudeBaseURLEnv,
	ClaudeAuthTokenEnv,
	ClaudeModelEnv,
	ClaudeDefaultHaikuModelEnv,
	ClaudeSubagentModelEnv,
	ClaudeAppendSystemPromptEnv,
}

type ClaudeSettings struct {
	Profiles []ClaudeProfileConfig `json:"profiles,omitempty"`
}

type ClaudeProfileConfig struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	AuthMode        string `json:"authMode,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	AuthToken       string `json:"authToken,omitempty"`
	Model           string `json:"model,omitempty"`
	SmallModel      string `json:"smallModel,omitempty"`
	SubagentModel   string `json:"subagentModel,omitempty"`
	Instruction     string `json:"instruction,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	VisionSupported bool   `json:"visionSupported,omitempty"`
}

type ClaudeProfile struct {
	ClaudeProfileConfig
	BuiltIn bool
}

func BuiltInClaudeProfile() ClaudeProfile {
	return ClaudeProfile{
		BuiltIn: true,
		ClaudeProfileConfig: ClaudeProfileConfig{
			ID:       ClaudeDefaultProfileID,
			Name:     ClaudeDefaultProfileName,
			AuthMode: ClaudeAuthModeInherit,
		},
	}
}

func NormalizeClaudeAuthMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ClaudeAuthModeInherit:
		return ClaudeAuthModeInherit
	case "auth-token", "auth token", ClaudeAuthModeAuthToken, "token":
		return ClaudeAuthModeAuthToken
	default:
		return ClaudeAuthModeInherit
	}
}

func NormalizeClaudeReasoningEffort(value string) string {
	return state.NormalizeClaudeReasoningEffort(value)
}

func SplitClaudeExtendedContextSuffix(model string) (string, bool) {
	model = strings.TrimSpace(model)
	extended := false
	for strings.HasSuffix(strings.ToLower(model), "[1m]") {
		model = strings.TrimSpace(model[:len(model)-len("[1m]")])
		extended = true
	}
	return model, extended
}

func CanonicalClaudeProfileID(value string) string {
	return canonicalProfileID(value, '-')
}

func IsBuiltInClaudeProfileID(value string) bool {
	return CanonicalClaudeProfileID(value) == ClaudeDefaultProfileID
}

func ClaudeProfileIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if id := CanonicalClaudeProfileID(name); id != "" {
		return id
	}
	sum := sha1.Sum([]byte(name))
	return "profile-" + hex.EncodeToString(sum[:])[:12]
}

func NormalizeClaudeProfiles(profiles []ClaudeProfileConfig) []ClaudeProfileConfig {
	if len(profiles) == 0 {
		return nil
	}
	normalized := make([]ClaudeProfileConfig, 0, len(profiles))
	used := map[string]struct{}{
		ClaudeDefaultProfileID: {},
	}
	for _, profile := range profiles {
		current := ClaudeProfileConfig{
			ID:              strings.TrimSpace(profile.ID),
			Name:            strings.TrimSpace(profile.Name),
			AuthMode:        NormalizeClaudeAuthMode(profile.AuthMode),
			BaseURL:         strings.TrimSpace(profile.BaseURL),
			AuthToken:       strings.TrimSpace(profile.AuthToken),
			Model:           strings.TrimSpace(profile.Model),
			SmallModel:      strings.TrimSpace(profile.SmallModel),
			SubagentModel:   strings.TrimSpace(profile.SubagentModel),
			Instruction:     strings.TrimSpace(profile.Instruction),
			ReasoningEffort: NormalizeClaudeReasoningEffort(profile.ReasoningEffort),
			VisionSupported: profile.VisionSupported,
		}
		current.ID = nextClaudeProfileID(current.ID, current.Name, used)
		if strings.TrimSpace(current.Name) == "" {
			current.Name = current.ID
		}
		normalized = append(normalized, current)
	}
	return normalized
}

func ListClaudeProfiles(cfg AppConfig) []ClaudeProfile {
	profiles := []ClaudeProfile{BuiltInClaudeProfile()}
	for _, profile := range NormalizeClaudeProfiles(cfg.Claude.Profiles) {
		profiles = append(profiles, ClaudeProfile{ClaudeProfileConfig: profile})
	}
	return profiles
}

func IndexOfClaudeProfile(profiles []ClaudeProfileConfig, profileID string) int {
	profileID = CanonicalClaudeProfileID(profileID)
	if profileID == "" || profileID == ClaudeDefaultProfileID {
		return -1
	}
	for index, profile := range profiles {
		if CanonicalClaudeProfileID(profile.ID) == profileID {
			return index
		}
	}
	return -1
}

func ResolveClaudeProfile(cfg AppConfig, profileID string) (ClaudeProfile, bool) {
	profileID = CanonicalClaudeProfileID(profileID)
	if profileID == "" || profileID == ClaudeDefaultProfileID {
		return BuiltInClaudeProfile(), true
	}
	for _, profile := range NormalizeClaudeProfiles(cfg.Claude.Profiles) {
		if profile.ID == profileID {
			return ClaudeProfile{ClaudeProfileConfig: profile}, true
		}
	}
	return ClaudeProfile{}, false
}

func ApplyClaudeProfileLaunchEnv(baseEnv []string, profile ClaudeProfile) ([]string, error) {
	env := append([]string{}, baseEnv...)
	if profile.BuiltIn {
		return env, nil
	}
	env = removeEnvKeys(env, claudeProfileLaunchEnvKeys...)
	env = ApplyClaudeRuntimeSettingsEnv(env, ClaudeProfileRuntimeSettings(profile))
	if value := strings.TrimSpace(profile.Instruction); value != "" {
		env = upsertEnvValue(env, ClaudeAppendSystemPromptEnv, value)
	}
	return env, nil
}

func ApplyClaudeReasoningLaunchEnv(baseEnv []string, effort string) []string {
	return ApplyClaudeRuntimeSettingsEnv(baseEnv, ClaudeReasoningRuntimeSettings(effort))
}

func nextClaudeProfileID(id, name string, used map[string]struct{}) string {
	return nextCatalogID(CanonicalClaudeProfileID, "profile", id, name, used)
}

func removeEnvKeys(env []string, keys ...string) []string {
	if len(env) == 0 {
		return nil
	}
	if len(keys) == 0 {
		return append([]string{}, env...)
	}
	remove := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		remove[key] = struct{}{}
	}
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, drop := remove[key]; drop {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
