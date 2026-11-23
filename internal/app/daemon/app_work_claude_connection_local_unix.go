//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Native -p uses the main checkout's local file only for an owned repository;
// shared project settings still come from the actual starting directory.
func claudeConnectionLocalRoot(cwd, home string) string {
	path, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	for {
		marker := filepath.Join(path, ".git")
		info, err := os.Stat(marker)
		if err == nil {
			root := path
			if !info.IsDir() {
				raw, e := os.ReadFile(marker)
				if e != nil {
					return ""
				}
				gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
				if !ok {
					return ""
				}
				gitdir = strings.TrimSpace(gitdir)
				if !filepath.IsAbs(gitdir) {
					gitdir = filepath.Join(path, gitdir)
				}
				if common, e := os.ReadFile(filepath.Join(gitdir, "commondir")); e == nil {
					commonDir := strings.TrimSpace(string(common))
					if !filepath.IsAbs(commonDir) {
						commonDir = filepath.Join(gitdir, commonDir)
					}
					commonDir = filepath.Clean(commonDir)
					if filepath.Base(commonDir) != ".git" {
						return ""
					}
					root = filepath.Dir(commonDir)
				}
			}
			if filepath.Clean(root) == filepath.Clean(home) {
				return ""
			}
			for _, candidate := range []string{root, filepath.Join(root, ".git"), filepath.Join(root, ".claude")} {
				info, e := os.Stat(candidate)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return ""
				}
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || int(stat.Uid) != os.Getuid() {
					return ""
				}
			}
			return root
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}
