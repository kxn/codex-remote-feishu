package wrapper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

func (a *App) applyClaudeRuntimeSettingsOverlay(baseArgs, baseEnv []string) ([]string, []string) {
	args := append([]string{}, baseArgs...)
	env := config.ApplyClaudeRuntimeSettingsEnv(baseEnv, config.ClaudeRuntimeSettings{
		Env: map[string]string{
			config.ClaudeRuntimeSettingsJSONEnv: "",
		},
	})
	settings, ok := a.readClaudeRuntimeSettingsOverlay()
	if !ok {
		return args, env
	}
	settingsPath, err := a.writeClaudeRuntimeSettingsOverlay(settings)
	if err != nil {
		a.debugf("claude runtime settings overlay skipped: write failed err=%v", err)
		return args, env
	}
	args = append(args, "--settings", settingsPath)
	return args, env
}

func (a *App) readClaudeRuntimeSettingsOverlay() (config.ClaudeRuntimeSettings, bool) {
	raw := strings.TrimSpace(os.Getenv(config.ClaudeRuntimeSettingsJSONEnv))
	if raw == "" {
		return config.ClaudeRuntimeSettings{}, false
	}
	var settings config.ClaudeRuntimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		a.debugf("claude runtime settings overlay skipped: decode failed err=%v", err)
		return config.ClaudeRuntimeSettings{}, false
	}
	if settings.Empty() {
		return config.ClaudeRuntimeSettings{}, false
	}
	return settings, true
}

func (a *App) writeClaudeRuntimeSettingsOverlay(settings config.ClaudeRuntimeSettings) (string, error) {
	path, err := a.claudeRuntimeSettingsOverlayPath(settings)
	if err != nil {
		return "", err
	}
	if err := writeJSONFileAtomic(path, settings, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) claudeRuntimeSettingsOverlayPath(settings config.ClaudeRuntimeSettings) (string, error) {
	stateDir := strings.TrimSpace(a.config.RuntimePaths.StateDir)
	if stateDir == "" {
		return "", fmt.Errorf("claude runtime settings overlay path is unavailable")
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	name := "codex-remote-claude-settings-" + hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(stateDir, "claude-settings", name), nil
}
