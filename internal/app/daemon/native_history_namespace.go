package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"os"
	"path/filepath"
	"strings"
)

// nativeHistoryNamespace freezes backend history root and host, independent of
// account compatibility. Resolve relative env paths against native process cwd.
func nativeHistoryNamespace(backend agentproto.Backend, env []string, cwd, host string) (string, error) {
	if host == "" || !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("history requires host and absolute cwd")
	}
	root, err := nativeHistoryRoot(backend, env, cwd)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal([]string{string(backend), host, root})
	digest := sha256.Sum256(raw)
	return "history_" + hex.EncodeToString(digest[:]), nil
}

// nativeHistoryRoot uses exactly the same environment and canonicalization as
// the namespace identity. It is not an authorization to inspect history.
func nativeHistoryRoot(backend agentproto.Backend, env []string, cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("history requires absolute cwd")
	}
	values := map[string]string{}
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			values[k] = v
		}
	}
	home := strings.TrimSpace(values["HOME"])
	if home == "" {
		home = strings.TrimSpace(values["USERPROFILE"])
	}
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	var root string
	switch backend {
	case agentproto.BackendCodex:
		root = strings.TrimSpace(values["CODEX_HOME"])
		if root == "" {
			root = filepath.Join(home, ".codex")
		}
	case agentproto.BackendOpenCode:
		root = strings.TrimSpace(values["XDG_DATA_HOME"])
		if root == "" {
			root = filepath.Join(home, ".local", "share")
		}
		root = filepath.Join(root, "opencode")
	case agentproto.BackendClaude:
		root = strings.TrimSpace(values["CLAUDE_CONFIG_DIR"])
		if root == "" {
			root = filepath.Join(home, ".claude")
		}
		root = filepath.Join(root, "projects")
	default:
		return "", fmt.Errorf("unsupported native history backend")
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(cwd, root)
	}
	root = filepath.Clean(root)
	// Resolve existing ancestors to avoid aliasing the same history through a
	// symlink; the history directory itself need not exist before first launch.
	ancestor := root
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			root = resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				root = filepath.Join(root, suffix[i])
			}
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	return root, nil
}
