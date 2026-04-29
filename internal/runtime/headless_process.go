package relayruntime

import (
	"path/filepath"
	"strings"
)

const (
	HeadlessLaunchModeAppServer       = "app-server"
	HeadlessLaunchModeClaudeAppServer = "claude-app-server"
)

type HeadlessLaunchOptions struct {
	BinaryPath string
	ConfigPath string
	Env        []string
	Paths      Paths
	WorkDir    string
	InstanceID string
	LaunchMode string
	Args       []string
}

func StartDetachedWrapper(opts HeadlessLaunchOptions) (int, error) {
	logPath := filepath.Join(opts.Paths.LogsDir, "codex-remote-headless-"+sanitizeFilename(opts.InstanceID)+".log")
	args := buildHeadlessWrapperArgs(opts)
	return startRuntimeDetachedProcess(runtimeDetachedLaunchOptions{
		BinaryPath: opts.BinaryPath,
		Args:       args,
		Env:        opts.Env,
		ConfigPath: opts.ConfigPath,
		WorkDir:    opts.WorkDir,
		LogPath:    logPath,
		Paths:      opts.Paths,
	})
}

func buildHeadlessWrapperArgs(opts HeadlessLaunchOptions) []string {
	args := []string{normalizeHeadlessLaunchMode(opts.LaunchMode)}
	return append(args, opts.Args...)
}

func normalizeHeadlessLaunchMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case HeadlessLaunchModeClaudeAppServer:
		return HeadlessLaunchModeClaudeAppServer
	default:
		return HeadlessLaunchModeAppServer
	}
}

func sanitizeFilename(value string) string {
	if value == "" {
		return "unknown"
	}
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r)
		case r >= '0' && r <= '9':
			out = append(out, r)
		case r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
