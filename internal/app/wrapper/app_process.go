package wrapper

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type childLaunchOptions struct {
	HideWindow     bool
	CreateNoWindow bool
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseBoolEnv(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func startChild(cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return stdin, stdout, stderr, nil
}

func configureChildProcess(cmd *exec.Cmd, cfg Config) {
	applyChildLaunchOptions(cmd, childLaunchOptionsForAgent(cfg))
	if strings.EqualFold(strings.TrimSpace(cfg.AgentType), "claude") {
		configureClaudeChildEnv(cmd)
	}
}

func configureClaudeChildEnv(cmd *exec.Cmd) {
	// Set required env for Claude CLI SDK mode
	cmd.Env = append(cmd.Env, "CLAUDE_CODE_ENTRYPOINT=sdk-go")
	// Strip CLAUDECODE nesting guard so SDK subprocess works
	filtered := make([]string, 0, len(cmd.Env))
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered
}

func childLaunchOptionsForAgent(cfg Config) childLaunchOptions {
	if !cfg.Managed || !strings.EqualFold(strings.TrimSpace(cfg.Source), "headless") {
		return childLaunchOptions{}
	}
	return childLaunchOptions{
		HideWindow:     true,
		CreateNoWindow: true,
	}
}

func childEnvWithProxy(proxyEnv []string) []string {
	filtered := config.FilterEnvWithoutProxy(os.Environ())
	filtered = append(filtered, proxyEnv...)
	return filtered
}

func generateInstanceID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("inst-%s", hex.EncodeToString(bytes[:])), nil
}
