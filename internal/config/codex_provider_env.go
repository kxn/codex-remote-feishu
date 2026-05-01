package config

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

type CodexProviderEnvInfo struct {
	ConfigPath     string
	ActiveProfile  string
	ActiveProvider string
	RequiredEnvKey string
}

type codexConfigToml struct {
	Profile        string                            `toml:"profile"`
	ModelProvider  string                            `toml:"model_provider"`
	Profiles       map[string]codexProfileToml       `toml:"profiles"`
	ModelProviders map[string]codexModelProviderToml `toml:"model_providers"`
}

type codexProfileToml struct {
	ModelProvider string `toml:"model_provider"`
}

type codexModelProviderToml struct {
	EnvKey string `toml:"env_key"`
}

type codexLaunchOverrides struct {
	Profile          string
	ModelProvider    string
	ProfileProviders map[string]string
	ProviderEnvKeys  map[string]string
}

type codexShellType string

const (
	codexShellBash codexShellType = "bash"
	codexShellZsh  codexShellType = "zsh"
	codexShellSh   codexShellType = "sh"
)

const (
	codexConfigFileName        = "config.toml"
	codexShellLookupTimeout    = 10 * time.Second
	codexShellLookupValueStart = "__CODEX_REMOTE_ENV_VALUE_START__"
	codexShellLookupValueEnd   = "__CODEX_REMOTE_ENV_VALUE_END__"
)

var lookupUserShellEnvValue = lookupUserShellEnvValueReal

func LookupUserShellEnvValue(env []string, key string) (string, error) {
	return lookupUserShellEnvValue(env, key)
}

func BuildCodexChildEnv(currentEnv, proxyEnv, args []string) []string {
	env := FilterEnvWithoutProxy(append([]string{}, currentEnv...))
	env = append(env, proxyEnv...)
	env = SupplementDetachedPATH(env)
	supplemented, _ := supplementCodexProviderEnv(env, args)
	return supplemented
}

func SupplementDetachedPATH(env []string) []string {
	if shellPath, err := lookupUserShellEnvValue(env, "PATH"); err == nil {
		merged := mergePATHValue(lookupEnvValueOrEmpty(env, "PATH"), shellPath)
		if strings.TrimSpace(merged) != "" {
			return upsertEnvValue(env, "PATH", merged)
		}
	}
	current := normalizePathListBySeparator(lookupEnvValueOrEmpty(env, "PATH"), string(os.PathListSeparator))
	if len(current) == 0 {
		return env
	}
	return upsertEnvValue(env, "PATH", strings.Join(current, string(os.PathListSeparator)))
}

func ResolveCodexProviderEnv(args []string, env []string) (CodexProviderEnvInfo, error) {
	overrides := parseCodexLaunchOverrides(args)
	cfg := codexConfigToml{
		Profiles:       map[string]codexProfileToml{},
		ModelProviders: map[string]codexModelProviderToml{},
	}

	configPath, pathErr := resolveCodexConfigPath(env)
	if configPath != "" {
		loaded, loadErr := loadCodexConfigToml(configPath)
		if loadErr == nil {
			cfg = loaded
		}
		if pathErr == nil {
			pathErr = loadErr
		}
	}
	applyCodexLaunchOverrides(&cfg, overrides)

	info := CodexProviderEnvInfo{ConfigPath: configPath}
	info.ActiveProfile = chooseNonEmpty(overrides.Profile, cfg.Profile)

	profileProvider, err := activeProfileProvider(cfg, info.ActiveProfile)
	if err != nil {
		return info, err
	}

	info.ActiveProvider = chooseNonEmpty(overrides.ModelProvider, profileProvider, cfg.ModelProvider, "openai")
	if provider, ok := cfg.ModelProviders[info.ActiveProvider]; ok {
		info.RequiredEnvKey = strings.TrimSpace(provider.EnvKey)
	}
	return info, pathErr
}

func supplementCodexProviderEnv(env, args []string) ([]string, error) {
	info, err := ResolveCodexProviderEnv(args, env)
	requiredKey := strings.TrimSpace(info.RequiredEnvKey)
	if requiredKey == "" {
		return env, nil
	}
	if value, ok := lookupEnvValue(env, requiredKey); ok && strings.TrimSpace(value) != "" {
		return env, nil
	}
	value, lookupErr := lookupUserShellEnvValue(env, requiredKey)
	if lookupErr != nil {
		return env, lookupErr
	}
	if strings.TrimSpace(value) == "" {
		return env, nil
	}
	return upsertEnvValue(env, requiredKey, value), err
}

func resolveCodexConfigPath(env []string) (string, error) {
	envMap := envSliceToMap(env)
	if codexHome := strings.TrimSpace(envMap["CODEX_HOME"]); codexHome != "" {
		info, err := os.Stat(codexHome)
		if err != nil {
			return "", fmt.Errorf("resolve CODEX_HOME %q: %w", codexHome, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("CODEX_HOME %q is not a directory", codexHome)
		}
		return filepath.Join(codexHome, codexConfigFileName), nil
	}
	home := strings.TrimSpace(envMap["HOME"])
	if home == "" {
		home = currentUserHomeDir()
	}
	if home == "" {
		return "", fmt.Errorf("resolve codex home: home directory is unavailable")
	}
	return filepath.Join(home, ".codex", codexConfigFileName), nil
}

func currentUserHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return pathscope.ApplyPrefix(home)
	}
	if home := strings.TrimSpace(os.Getenv("USERPROFILE")); home != "" {
		return pathscope.ApplyPrefix(home)
	}
	if home, err := pathscope.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	current, err := user.Current()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(current.HomeDir)
}

func loadCodexConfigToml(path string) (codexConfigToml, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexConfigToml{
				Profiles:       map[string]codexProfileToml{},
				ModelProviders: map[string]codexModelProviderToml{},
			}, nil
		}
		return codexConfigToml{}, err
	}
	var cfg codexConfigToml
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		return codexConfigToml{}, err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]codexProfileToml{}
	}
	if cfg.ModelProviders == nil {
		cfg.ModelProviders = map[string]codexModelProviderToml{}
	}
	return cfg, nil
}

func activeProfileProvider(cfg codexConfigToml, activeProfile string) (string, error) {
	activeProfile = strings.TrimSpace(activeProfile)
	if activeProfile == "" {
		return "", nil
	}
	profile, ok := cfg.Profiles[activeProfile]
	if !ok {
		return "", fmt.Errorf("codex profile %q not found", activeProfile)
	}
	return strings.TrimSpace(profile.ModelProvider), nil
}

func applyCodexLaunchOverrides(cfg *codexConfigToml, overrides codexLaunchOverrides) {
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]codexProfileToml{}
	}
	if cfg.ModelProviders == nil {
		cfg.ModelProviders = map[string]codexModelProviderToml{}
	}
	if overrides.Profile != "" {
		cfg.Profile = overrides.Profile
	}
	if overrides.ModelProvider != "" {
		cfg.ModelProvider = overrides.ModelProvider
	}
	for profileName, providerID := range overrides.ProfileProviders {
		profile := cfg.Profiles[profileName]
		profile.ModelProvider = providerID
		cfg.Profiles[profileName] = profile
	}
	for providerID, envKey := range overrides.ProviderEnvKeys {
		provider := cfg.ModelProviders[providerID]
		provider.EnvKey = envKey
		cfg.ModelProviders[providerID] = provider
	}
}

func parseCodexLaunchOverrides(args []string) codexLaunchOverrides {
	overrides := codexLaunchOverrides{
		ProfileProviders: map[string]string{},
		ProviderEnvKeys:  map[string]string{},
	}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "--profile" && i+1 < len(args):
			overrides.Profile = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--profile="):
			overrides.Profile = strings.TrimSpace(strings.TrimPrefix(arg, "--profile="))
		case arg == "-c" && i+1 < len(args):
			applyCodexOverride(&overrides, args[i+1])
			i++
		case arg == "--config" && i+1 < len(args):
			applyCodexOverride(&overrides, args[i+1])
			i++
		case strings.HasPrefix(arg, "--config="):
			applyCodexOverride(&overrides, strings.TrimPrefix(arg, "--config="))
		case len(arg) > 2 && strings.HasPrefix(arg, "-c"):
			applyCodexOverride(&overrides, arg[2:])
		}
	}
	return overrides
}

func applyCodexOverride(overrides *codexLaunchOverrides, raw string) {
	key, value, ok := splitCodexOverride(raw)
	if !ok {
		return
	}
	switch {
	case key == "profile":
		overrides.Profile = value
	case key == "model_provider":
		overrides.ModelProvider = value
	case strings.HasPrefix(key, "profiles.") && strings.HasSuffix(key, ".model_provider"):
		parts := strings.Split(key, ".")
		if len(parts) != 3 || parts[1] == "" {
			return
		}
		overrides.ProfileProviders[parts[1]] = value
	case strings.HasPrefix(key, "model_providers.") && strings.HasSuffix(key, ".env_key"):
		parts := strings.Split(key, ".")
		if len(parts) != 3 || parts[1] == "" {
			return
		}
		overrides.ProviderEnvKeys[parts[1]] = value
	}
}

func splitCodexOverride(raw string) (string, string, bool) {
	key, value, ok := strings.Cut(raw, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	return key, parseCodexOverrideString(value), true
}

func parseCodexOverrideString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var holder struct {
		Value string `toml:"value"`
	}
	if _, err := toml.Decode("value = "+raw, &holder); err == nil {
		return holder.Value
	}
	return strings.Trim(raw, `"'`)
}

func lookupUserShellEnvValueReal(env []string, key string) (string, error) {
	if !isValidShellEnvKey(key) {
		return "", fmt.Errorf("lookup codex env %q: invalid key", key)
	}
	shellType, shellPath := detectUserShell(env)
	if shellPath == "" {
		return "", fmt.Errorf("lookup codex env %q: no supported user shell found", key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexShellLookupTimeout)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, shellPath, shellLookupArgs(shellType, key)...)
	cmd.Env = ensureHomeEnv(append([]string{}, env...))
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("lookup codex env %q: shell lookup timed out", key)
	}
	if err != nil {
		return "", fmt.Errorf("lookup codex env %q via %s: %w", key, shellPath, err)
	}
	return parseShellLookupValue(output), nil
}

func shellLookupArgs(shellType codexShellType, key string) []string {
	script := shellLookupScript(shellType, key)
	switch shellType {
	case codexShellBash, codexShellZsh:
		return []string{"-ilc", script}
	default:
		return []string{"-lc", script}
	}
}

func detectUserShell(env []string) (codexShellType, string) {
	envMap := envSliceToMap(env)
	candidates := []string{
		currentUserShellFromPasswd(),
		strings.TrimSpace(envMap["SHELL"]),
		"/bin/bash",
		"/bin/zsh",
		"/bin/sh",
	}
	for _, candidate := range candidates {
		shellType, shellPath := supportedShell(candidate)
		if shellPath != "" {
			return shellType, shellPath
		}
	}
	return "", ""
}

func currentUserShellFromPasswd() string {
	current, err := user.Current()
	if err != nil || strings.TrimSpace(current.Uid) == "" {
		return ""
	}
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 7 || fields[2] != current.Uid {
			continue
		}
		return strings.TrimSpace(fields[6])
	}
	return ""
}

func supportedShell(path string) (codexShellType, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	if !strings.Contains(path, string(filepath.Separator)) {
		resolved, err := exec.LookPath(path)
		if err != nil {
			return "", ""
		}
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", ""
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "bash":
		return codexShellBash, path
	case "zsh":
		return codexShellZsh, path
	case "sh", "dash", "ash":
		return codexShellSh, path
	default:
		return "", ""
	}
}

func shellLookupScript(shellType codexShellType, key string) string {
	var builder strings.Builder
	switch shellType {
	case codexShellBash:
		builder.WriteString("if [ -z \"$BASH_ENV\" ] && [ -r \"$HOME/.bashrc\" ]; then . \"$HOME/.bashrc\" >/dev/null 2>&1 || true; fi\n")
	case codexShellZsh:
		builder.WriteString("if [ -n \"$ZDOTDIR\" ]; then rc=\"$ZDOTDIR/.zshrc\"; else rc=\"$HOME/.zshrc\"; fi\n")
		builder.WriteString("if [ -r \"$rc\" ]; then . \"$rc\" >/dev/null 2>&1 || true; fi\n")
	case codexShellSh:
		builder.WriteString("if [ -n \"$ENV\" ] && [ -r \"$ENV\" ]; then . \"$ENV\" >/dev/null 2>&1 || true; fi\n")
	}
	builder.WriteString("printf '%s' '" + codexShellLookupValueStart + "'\n")
	builder.WriteString("printf '%s' \"${" + key + "-}\"\n")
	builder.WriteString("printf '%s' '" + codexShellLookupValueEnd + "'\n")
	return builder.String()
}

func parseShellLookupValue(raw []byte) string {
	output := string(raw)
	start := strings.LastIndex(output, codexShellLookupValueStart)
	if start < 0 {
		return ""
	}
	start += len(codexShellLookupValueStart)
	end := strings.Index(output[start:], codexShellLookupValueEnd)
	if end < 0 {
		return ""
	}
	return output[start : start+end]
}

func ensureHomeEnv(env []string) []string {
	if value, ok := lookupEnvValue(env, "HOME"); ok && strings.TrimSpace(value) != "" {
		return env
	}
	home := currentUserHomeDir()
	if strings.TrimSpace(home) == "" {
		return env
	}
	return append(env, "HOME="+home)
}

func lookupEnvValueOrEmpty(env []string, key string) string {
	value, _ := lookupEnvValue(env, key)
	return value
}

func lookupEnvValue(env []string, key string) (string, bool) {
	for _, entry := range env {
		currentKey, currentValue, ok := strings.Cut(entry, "=")
		if !ok || currentKey != key {
			continue
		}
		return currentValue, true
	}
	return "", false
}

func UpsertEnvValue(env []string, key, value string) []string {
	updated := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		currentKey, _, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			if !replaced {
				updated = append(updated, key+"="+value)
				replaced = true
			}
			continue
		}
		updated = append(updated, entry)
	}
	if !replaced {
		updated = append(updated, key+"="+value)
	}
	return updated
}

func upsertEnvValue(env []string, key, value string) []string {
	return UpsertEnvValue(env, key, value)
}

func envSliceToMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		values[key] = value
	}
	return values
}

func mergePATHValue(current, shell string) string {
	separator := string(os.PathListSeparator)
	currentParts := normalizePathListBySeparator(current, separator)
	shellParts := normalizePathListBySeparator(shell, separator)
	if len(currentParts) == 0 {
		return strings.Join(shellParts, separator)
	}
	if len(shellParts) == 0 {
		return strings.Join(currentParts, separator)
	}
	seen := make(map[string]struct{}, len(currentParts)+len(shellParts))
	out := make([]string, 0, len(currentParts)+len(shellParts))
	appendPart := func(part string) {
		key := part
		if os.PathListSeparator == ';' {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	for _, part := range currentParts {
		appendPart(part)
	}
	for _, part := range shellParts {
		appendPart(part)
	}
	return strings.Join(out, separator)
}

func normalizePathListBySeparator(value, separator string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if separator == "" {
		separator = string(os.PathListSeparator)
	}
	parts := strings.Split(value, separator)
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := part
		if separator == ";" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return out
}

func isValidShellEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case i == 0 && (r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')):
		case i > 0 && (r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9')):
		default:
			return false
		}
	}
	return true
}
