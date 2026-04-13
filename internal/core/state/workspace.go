package state

import (
	"path"
	"path/filepath"
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
