package state

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func NormalizeWorkspaceKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	normalized := filepath.Clean(value)
	if normalized == "." {
		return ""
	}
	return filepath.ToSlash(normalized)
}

func ResolveWorkspaceKey(values ...string) string {
	for _, value := range values {
		if normalized := NormalizeWorkspaceKey(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func ResolveWorkspaceClaimKey(value string) string {
	return ResolveWorkspaceClaimKeyForGOOS(runtime.GOOS, value)
}

func ResolveWorkspaceClaimKeyForGOOS(goos, value string) string {
	raw := strings.TrimSpace(value)
	normalized := ResolveWorkspaceKey(raw)
	if normalized == "" {
		return ""
	}
	if !ShouldResolveWorkspacePathOnHost(goos, raw) {
		return normalized
	}
	if resolved, err := ResolveWorkspaceRootOnHost(raw); err == nil {
		if resolved = ResolveWorkspaceKey(resolved); resolved != "" {
			return resolved
		}
	}
	return normalized
}

func ShouldResolveWorkspacePathOnHost(goos, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "windows":
		if IsWindowsVolumePath(raw) || strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, `//`) || strings.HasPrefix(raw, `\`) {
			return true
		}
	default:
		if strings.HasPrefix(raw, "/") {
			return true
		}
	}
	switch raw {
	case ".", "..":
		return true
	}
	return strings.HasPrefix(raw, "./") ||
		strings.HasPrefix(raw, "../") ||
		strings.HasPrefix(raw, `.\\`) ||
		strings.HasPrefix(raw, `..\\`)
}

func IsWindowsVolumePath(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	drive := value[0]
	return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
}

func ResolveHeadlessResumeWorkspaceKey(workspaceKey, threadCWD string) string {
	workspaceKey = ResolveWorkspaceClaimKey(workspaceKey)
	threadCWD = ResolveWorkspaceClaimKey(threadCWD)
	if workspaceKey == "" {
		return threadCWD
	}
	if threadCWD == "" || threadCWD == workspaceKey {
		return workspaceKey
	}
	if strings.HasPrefix(threadCWD, workspaceKey+"/") {
		return workspaceKey
	}
	return threadCWD
}

func ResolveWorkspaceRootOnHost(value string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	normalized := NormalizeWorkspaceKey(absolute)
	if resolved, err := filepath.EvalSymlinks(normalized); err == nil {
		normalized = NormalizeWorkspaceKey(resolved)
	}
	return normalized, nil
}

func WorkspaceShortName(value string) string {
	key := ResolveWorkspaceKey(value)
	if key == "" {
		return ""
	}
	short := strings.TrimSpace(path.Base(key))
	if short == "" || short == "." || short == "/" {
		return key
	}
	return short
}
