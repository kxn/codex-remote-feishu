package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type claudeConnectionSettings struct {
	Env                 map[string]string `json:"env"`
	APIKeyHelper        string            `json:"apiKeyHelper"`
	AWSAuthRefresh      string            `json:"awsAuthRefresh"`
	AWSCredentialExport string            `json:"awsCredentialExport"`
}

// Only effective connection facts enter the persisted digest. Model, effort,
// permissions, prompts and shadowed settings must not discard private history.
func teamClaudeConnectionIdentity(env []string, profile config.ClaudeProfile, workingDirs ...string) (string, error) {
	values := map[string]string{}
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			values[k] = v
		}
	}
	home := values["HOME"]
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	values["HOME"] = home
	dir := values[config.ClaudeConfigDirEnv]
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	cwd := ""
	if len(workingDirs) > 0 {
		cwd = workingDirs[0]
	}
	settings, err := effectiveClaudeConnectionSettings(values, profile, dir, cwd, claudeManagedSettingsDir())
	if err != nil {
		return "", err
	}
	connection := effectiveClaudeConnectionEnv(settings.Env)
	connection["configDir"] = dir
	if connection["provider"] != "anthropic" {
		settings.APIKeyHelper = ""
	}
	if connection["provider"] != "bedrock" {
		settings.AWSAuthRefresh = ""
		settings.AWSCredentialExport = ""
	}
	var auth any
	// An explicit API/provider credential shadows native OAuth. Do not let a
	// refresh or login to an unused native account reset an API-profile session.
	external := connection[config.ClaudeAuthTokenEnv] != "" || connection["ANTHROPIC_API_KEY"] != "" || connection["CLAUDE_CODE_OAUTH_TOKEN"] != "" || settings.APIKeyHelper != ""
	external = external || connection["provider"] != "anthropic"
	if !external {
		accountPath := filepath.Join(home, ".claude.json")
		if values[config.ClaudeConfigDirEnv] != "" {
			accountPath = filepath.Join(dir, ".claude.json")
		}
		account, e := readClaudeConnectionFile(accountPath)
		if e != nil {
			return "", e
		}
		var accountConfig struct {
			OAuthAccount struct {
				AccountUUID      string `json:"accountUuid"`
				Email            string `json:"emailAddress"`
				OrganizationUUID string `json:"organizationUuid"`
			} `json:"oauthAccount"`
		}
		_ = json.Unmarshal(account, &accountConfig)
		if accountConfig.OAuthAccount.AccountUUID != "" {
			auth = accountConfig.OAuthAccount
		} else {
			credential, e := readClaudeConnectionFile(filepath.Join(dir, ".credentials.json"))
			if e != nil {
				return "", e
			}
			if len(credential) == 0 && !claudeHasAuthenticationHeader(connection["ANTHROPIC_CUSTOM_HEADERS"]) {
				return "", nil
			}
			auth = credential
		}
	}
	// An exported API key takes precedence over apiKeyHelper; hashing an unused
	// helper would treat editing it as an account change.
	if connection["ANTHROPIC_API_KEY"] != "" || connection[config.ClaudeAuthTokenEnv] != "" {
		settings.APIKeyHelper = ""
	}
	raw, err := json.Marshal(struct {
		Connection                                        map[string]string
		Auth                                              any
		APIKeyHelper, AWSAuthRefresh, AWSCredentialExport string
	}{connection, auth, settings.APIKeyHelper, settings.AWSAuthRefresh, settings.AWSCredentialExport})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Claude's -p launch uses shell -> user -> project -> local -> --settings ->
// managed precedence for env. The wrapper's profile overlay is --settings,
// not merely a shell export. Empty env values explicitly unset lower values.
func effectiveClaudeConnectionSettings(values map[string]string, profile config.ClaudeProfile, dir, cwd, managedDir string) (claudeConnectionSettings, error) {
	result := claudeConnectionSettings{Env: map[string]string{}}
	for key, value := range values {
		result.Env[key] = value
	}
	// Match ApplyClaudeProfileLaunchEnv: a custom profile clears the old
	// shell route/token before native settings and its own overlay apply.
	if !profile.BuiltIn {
		delete(result.Env, config.ClaudeBaseURLEnv)
		delete(result.Env, config.ClaudeAuthTokenEnv)
	}
	merge := func(path string, project bool) error {
		raw, err := readClaudeConnectionFile(path)
		if err != nil {
			return err
		}
		if len(raw) == 0 {
			return nil
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			if managedDir != "" && strings.HasPrefix(path, filepath.Clean(managedDir)+string(filepath.Separator)) {
				return fmt.Errorf("Claude managed settings are not a JSON object")
			}
			return nil // Native -p ignores invalid user/project settings.
		}
		var nextEnv map[string]json.RawMessage
		_ = json.Unmarshal(fields["env"], &nextEnv)
		for key, rawValue := range nextEnv {
			var value string
			if json.Unmarshal(rawValue, &value) != nil {
				continue
			}
			if project && (key == "HOME" || key == config.ClaudeConfigDirEnv || strings.HasPrefix(key, "CLAUDE_CODE_OAUTH_")) {
				continue
			}
			result.Env[key] = value
		}
		for key, target := range map[string]*string{"apiKeyHelper": &result.APIKeyHelper, "awsAuthRefresh": &result.AWSAuthRefresh, "awsCredentialExport": &result.AWSCredentialExport} {
			if value, present := fields[key]; present {
				var text string
				if json.Unmarshal(value, &text) == nil {
					*target = text
				}
			}
		}

		return nil
	}
	if err := merge(filepath.Join(dir, "settings.json"), false); err != nil {
		return result, err
	}
	if cwd != "" {
		if err := merge(filepath.Join(cwd, ".claude", "settings.json"), true); err != nil {
			return result, err
		}
		if err := merge(filepath.Join(cwd, ".claude", "settings.local.json"), true); err != nil {
			return result, err
		}
		// Current native Claude retains the legacy cwd-local file, then
		// applies an owned repository/main-checkout local file above it.
		if root := claudeConnectionLocalRoot(cwd, values["HOME"]); root != "" && filepath.Clean(root) != filepath.Clean(cwd) {
			if err := merge(filepath.Join(root, ".claude", "settings.local.json"), true); err != nil {
				return result, err
			}
		}
	}
	overlay := config.ClaudeProfileRuntimeSettings(profile)
	generated := !overlay.Empty() || (profile.BuiltIn && (profile.Model != "" || profile.ReasoningEffort != ""))
	if !generated {
		var inherited config.ClaudeRuntimeSettings
		if json.Unmarshal([]byte(values[config.ClaudeRuntimeSettingsJSONEnv]), &inherited) == nil {
			overlay = inherited
		}
	}
	for key, value := range overlay.Env {
		result.Env[key] = value
	}
	if managedDir != "" {
		if err := merge(filepath.Join(managedDir, "managed-settings.json"), false); err != nil {
			return result, err
		}
		entries, err := os.ReadDir(filepath.Join(managedDir, "managed-settings.d"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && strings.HasSuffix(entry.Name(), ".json") {
				if err = merge(filepath.Join(managedDir, "managed-settings.d", entry.Name()), false); err != nil {
					return result, err
				}
			}
		}
	}
	return result, nil
}

func claudeHasAuthenticationHeader(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(value) != "" && (strings.EqualFold(strings.TrimSpace(key), "Authorization") || strings.EqualFold(strings.TrimSpace(key), "x-api-key")) {
			return true
		}
	}
	return false
}

func effectiveClaudeConnectionEnv(env map[string]string) map[string]string {
	enabled := func(key string) bool {
		switch strings.ToLower(strings.TrimSpace(env[key])) {
		case "1", "true", "yes", "on":
			return true
		}
		return false
	}
	selected := []string{config.ClaudeBaseURLEnv, config.ClaudeAuthTokenEnv, "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_CUSTOM_HEADERS"}
	provider := "anthropic"
	switch {
	case enabled("CLAUDE_CODE_USE_BEDROCK"):
		provider = "bedrock"
		selected = []string{"ANTHROPIC_BEDROCK_BASE_URL", "AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}
	case enabled("CLAUDE_CODE_USE_VERTEX"):
		provider = "vertex"
		selected = []string{"ANTHROPIC_VERTEX_BASE_URL", "GOOGLE_APPLICATION_CREDENTIALS", "ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION"}
	case enabled("CLAUDE_CODE_USE_FOUNDRY"):
		provider = "foundry"
		selected = []string{"ANTHROPIC_FOUNDRY_BASE_URL", "ANTHROPIC_FOUNDRY_RESOURCE", "ANTHROPIC_FOUNDRY_API_KEY"}
	}
	result := map[string]string{"provider": provider}
	for _, key := range selected {
		if value := env[key]; value != "" {
			result[key] = value
		}
	}
	return result
}

func claudeManagedSettingsDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}
func readClaudeConnectionFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}
