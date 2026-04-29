package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ClaudeDefaultProfileID   = "default"
	ClaudeDefaultProfileName = "默认"

	ClaudeAuthModeInherit   = "inherit"
	ClaudeAuthModeAuthToken = "auth_token"

	ClaudeConfigDirEnv         = "CLAUDE_CONFIG_DIR"
	ClaudeBaseURLEnv           = "ANTHROPIC_BASE_URL"
	ClaudeAuthTokenEnv         = "ANTHROPIC_AUTH_TOKEN"
	ClaudeModelEnv             = "ANTHROPIC_MODEL"
	ClaudeDefaultHaikuModelEnv = "ANTHROPIC_DEFAULT_HAIKU_MODEL"
)

var claudeProfileLaunchEnvKeys = []string{
	ClaudeConfigDirEnv,
	ClaudeBaseURLEnv,
	ClaudeAuthTokenEnv,
	ClaudeModelEnv,
	ClaudeDefaultHaikuModelEnv,
}

type ClaudeSettings struct {
	Profiles []ClaudeProfileConfig `json:"profiles,omitempty"`
}

type ClaudeProfileConfig struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	AuthMode   string `json:"authMode,omitempty"`
	BaseURL    string `json:"baseURL,omitempty"`
	AuthToken  string `json:"authToken,omitempty"`
	Model      string `json:"model,omitempty"`
	SmallModel string `json:"smallModel,omitempty"`
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

func CanonicalClaudeProfileID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func IsBuiltInClaudeProfileID(value string) bool {
	return CanonicalClaudeProfileID(value) == ClaudeDefaultProfileID
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
			ID:         strings.TrimSpace(profile.ID),
			Name:       strings.TrimSpace(profile.Name),
			AuthMode:   NormalizeClaudeAuthMode(profile.AuthMode),
			BaseURL:    strings.TrimSpace(profile.BaseURL),
			AuthToken:  strings.TrimSpace(profile.AuthToken),
			Model:      strings.TrimSpace(profile.Model),
			SmallModel: strings.TrimSpace(profile.SmallModel),
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

func NextClaudeProfileID(existing []ClaudeProfileConfig, requestedID, requestedName string) string {
	used := map[string]struct{}{
		ClaudeDefaultProfileID: {},
	}
	for _, profile := range NormalizeClaudeProfiles(existing) {
		used[profile.ID] = struct{}{}
	}
	return nextClaudeProfileID(requestedID, requestedName, used)
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

func ClaudeProfileRuntimeConfigDir(stateDir, profileID string) string {
	profileID = CanonicalClaudeProfileID(profileID)
	if profileID == "" || profileID == ClaudeDefaultProfileID {
		return ""
	}
	return filepath.Join(strings.TrimSpace(stateDir), "claude", "profiles", profileID)
}

func ApplyClaudeProfileLaunchEnv(baseEnv []string, profile ClaudeProfile, stateDir string) ([]string, error) {
	env := append([]string{}, baseEnv...)
	if profile.BuiltIn {
		return env, nil
	}
	configDir := ClaudeProfileRuntimeConfigDir(stateDir, profile.ID)
	if strings.TrimSpace(configDir) == "" {
		return nil, fmt.Errorf("claude profile %q requires a runtime config dir", strings.TrimSpace(profile.ID))
	}
	env = removeEnvKeys(env, claudeProfileLaunchEnvKeys...)
	env = upsertEnvValue(env, ClaudeConfigDirEnv, configDir)
	if value := strings.TrimSpace(profile.BaseURL); value != "" {
		env = upsertEnvValue(env, ClaudeBaseURLEnv, value)
	}
	if NormalizeClaudeAuthMode(profile.AuthMode) == ClaudeAuthModeAuthToken {
		if value := strings.TrimSpace(profile.AuthToken); value != "" {
			env = upsertEnvValue(env, ClaudeAuthTokenEnv, value)
		}
	}
	if value := strings.TrimSpace(profile.Model); value != "" {
		env = upsertEnvValue(env, ClaudeModelEnv, value)
	}
	if value := strings.TrimSpace(profile.SmallModel); value != "" {
		env = upsertEnvValue(env, ClaudeDefaultHaikuModelEnv, value)
	}
	return env, nil
}

func nextClaudeProfileID(id, name string, used map[string]struct{}) string {
	base := CanonicalClaudeProfileID(chooseNonEmpty(id, name, "profile"))
	if base == "" {
		base = "profile"
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
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
