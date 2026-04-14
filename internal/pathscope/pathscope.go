package pathscope

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvFSPrefix = "CODEX_REMOTE_FS_PREFIX"
	EnvFSStrict = "CODEX_REMOTE_FS_STRICT"
)

func PrefixRoot() string {
	value := strings.TrimSpace(os.Getenv(EnvFSPrefix))
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func UserHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ApplyPrefix(strings.TrimSpace(home)), nil
}

// ApplyPrefix remaps an absolute path under CODEX_REMOTE_FS_PREFIX when set.
// Relative paths are returned as-is.
func ApplyPrefix(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	prefix := PrefixRoot()
	if prefix == "" || !filepath.IsAbs(path) {
		return path
	}
	if withinPrefix(path, prefix) {
		return path
	}
	relative := strings.TrimLeft(path, string(filepath.Separator))
	if relative == "" {
		return prefix
	}
	return filepath.Join(prefix, relative)
}

// EnsureWritePath enforces strict path writes when CODEX_REMOTE_FS_STRICT is
// enabled. It only checks absolute paths.
func EnsureWritePath(path string) error {
	if !strictEnabled() {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil
	}
	prefix := PrefixRoot()
	if prefix == "" {
		return fmt.Errorf("%s is enabled but %s is empty", EnvFSStrict, EnvFSPrefix)
	}
	if withinPrefix(path, prefix) {
		return nil
	}
	return fmt.Errorf("write path %q escapes %s %q", path, EnvFSPrefix, prefix)
}

func strictEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvFSStrict))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func withinPrefix(path, prefix string) bool {
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)
	if path == prefix {
		return true
	}
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
