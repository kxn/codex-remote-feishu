package testutil

import (
	"path/filepath"
	"runtime"
	"strings"
)

func RootedPath(parts ...string) string {
	all := make([]string, 0, len(parts)+1)
	all = append(all, string(filepath.Separator))
	all = append(all, parts...)
	return filepath.Join(all...)
}

func CanonicalPath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func SamePath(left, right string) bool {
	return CanonicalPath(left) == CanonicalPath(right)
}
