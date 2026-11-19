package appserverargs

import (
	"fmt"
	"strings"
)

// PrivateStdioArgs keeps the relay's child process out of Codex's shared
// daemon and listener lifecycle. Config values must be skipped as values,
// even when they happen to contain a subcommand or listener flag.
func PrivateStdioArgs(args []string) ([]string, error) {
	match, ok := Find(args)
	if !ok || match.Mode != ModeCodex {
		return append([]string{}, args...), nil
	}
	hasStdio := false
	for i := match.Index + 1; i < len(args); i++ {
		key, value, equals := strings.Cut(args[i], "=")
		switch key {
		case "--remote-control", "--managed-daemon":
			return nil, fmt.Errorf("codex-remote app-server must remain private; %s is unsupported", key)
		case "--listen", "-c", "--config", "-C", "--cd", "--code-mode-host",
			"--ws-auth", "--ws-token-file", "--ws-token-sha256", "--ws-shared-secret-file",
			"--ws-issuer", "--ws-audience", "--ws-max-clock-skew-seconds":
			if !equals {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("missing value for %s", key)
				}
				i++
				value = args[i]
			}
			if key == "--listen" {
				if value != "stdio://" {
					return nil, fmt.Errorf("codex-remote app-server only supports --listen stdio://")
				}
				hasStdio = true
			}
		case "--stdio":
			hasStdio = true
		default:
			if !strings.HasPrefix(key, "-") || key == "--" {
				return nil, fmt.Errorf("codex-remote only wraps a private stdio app-server; app-server subcommands are unsupported")
			}
		}
	}
	out := append([]string{}, args...)
	if !hasStdio {
		out = append(out, "--listen=stdio://")
	}
	return out, nil
}

// PrivateStdioEnv disables inherited Remote Control enrollment for this
// process only, without changing the user's persisted Codex preferences.
func PrivateStdioEnv(env []string) []string {
	const key = "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED"
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			out = append(out, entry)
		}
	}
	return append(out, key+"=1")
}
