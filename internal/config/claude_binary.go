package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveClaudeBinary resolves the Claude executable path for daemon-owned
// launch paths. It prefers an explicit CLAUDE_BIN, then login-shell PATH, then
// the current process PATH.
func ResolveClaudeBinary(env []string) (string, error) {
	env = ensureHomeEnv(append([]string{}, env...))
	if configured, ok := lookupEnvValue(env, ClaudeBinaryEnv); ok && strings.TrimSpace(configured) != "" {
		resolved, err := resolveClaudeBinaryCandidate(env, configured)
		if err != nil {
			return "", fmt.Errorf("resolve %s %q: %w", ClaudeBinaryEnv, strings.TrimSpace(configured), err)
		}
		return resolved, nil
	}
	if resolved, err := resolveExecutableFromShellPATH(env, "claude"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved, nil
	}
	if resolved, err := resolveExecutableFromEnvPATH(env, "claude"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved, nil
	}
	return "", fmt.Errorf("claude executable not found")
}

func resolveClaudeBinaryCandidate(env []string, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("empty executable")
	}
	if looksLikeExecutablePath(value) {
		return resolveExplicitExecutablePath(value)
	}
	if resolved, err := resolveExecutableFromShellPATH(env, value); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved, nil
	}
	return resolveExecutableFromEnvPATH(env, value)
}

func resolveExecutableFromShellPATH(env []string, name string) (string, error) {
	pathValue, err := lookupUserShellEnvValue(env, "PATH")
	if err != nil {
		return "", err
	}
	return resolveExecutableFromPATHValue(name, pathValue, lookupEnvValueOrEmpty(env, "PATHEXT"))
}

func resolveExecutableFromEnvPATH(env []string, name string) (string, error) {
	return resolveExecutableFromPATHValue(name, lookupEnvValueOrEmpty(env, "PATH"), lookupEnvValueOrEmpty(env, "PATHEXT"))
}

func resolveExecutableFromPATHValue(name, pathValue, pathExt string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty executable")
	}
	if looksLikeExecutablePath(name) {
		return resolveExplicitExecutablePath(name)
	}
	for _, dir := range splitPathEntries(pathValue) {
		for _, candidate := range executableCandidatesInDir(dir, name, pathExt) {
			if resolved, ok := usableExecutablePath(candidate); ok {
				return resolved, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func splitPathEntries(pathValue string) []string {
	parts := strings.Split(pathValue, string(os.PathListSeparator))
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			part = "."
		}
		values = append(values, part)
	}
	return values
}

func executableCandidatesInDir(dir, name, pathExt string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, name)}
	}
	extensions := windowsExecutableExtensions(pathExt)
	if hasKnownWindowsExecutableExtension(name, extensions) {
		return []string{filepath.Join(dir, name)}
	}
	values := make([]string, 0, len(extensions))
	for _, ext := range extensions {
		values = append(values, filepath.Join(dir, name+ext))
	}
	return values
}

func windowsExecutableExtensions(pathExt string) []string {
	if strings.TrimSpace(pathExt) == "" {
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	rawParts := strings.Split(pathExt, ";")
	values := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		values = append(values, strings.ToLower(part))
	}
	if len(values) == 0 {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	return values
}

func hasKnownWindowsExecutableExtension(name string, extensions []string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func looksLikeExecutablePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return filepath.IsAbs(value) ||
		filepath.VolumeName(value) != "" ||
		strings.ContainsRune(value, filepath.Separator) ||
		(filepath.Separator != '/' && strings.ContainsRune(value, '/')) ||
		(filepath.Separator != '\\' && strings.ContainsRune(value, '\\'))
}

func resolveExplicitExecutablePath(path string) (string, error) {
	resolved, ok := usableExecutablePath(path)
	if !ok {
		return "", exec.ErrNotFound
	}
	return resolved, nil
}

func usableExecutablePath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return "", false
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(resolved) != "" {
		path = resolved
	}
	return filepath.Clean(path), true
}
