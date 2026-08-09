package config

import (
	"fmt"
	"strings"
)

// ResolveOpenCodeBinary resolves the OpenCode executable for daemon-owned
// launches. It prefers OPENCODE_BIN, then config openCode.binaryPath, then
// login-shell PATH, then the current process PATH.
func ResolveOpenCodeBinary(env []string, configuredBinary string) (string, error) {
	env = ensureHomeEnv(append([]string{}, env...))
	if configured, ok := lookupEnvValue(env, OpenCodeBinaryEnv); ok && strings.TrimSpace(configured) != "" {
		resolved, err := resolveOpenCodeBinaryCandidate(env, configured)
		if err != nil {
			return "", fmt.Errorf("resolve %s %q: %w", OpenCodeBinaryEnv, strings.TrimSpace(configured), err)
		}
		return resolved, nil
	}
	if configured := strings.TrimSpace(configuredBinary); configured != "" {
		resolved, err := resolveOpenCodeBinaryCandidate(env, configured)
		if err != nil {
			return "", fmt.Errorf("resolve openCode.binaryPath %q: %w", configured, err)
		}
		return resolved, nil
	}
	if resolved, err := resolveExecutableFromShellPATH(env, "opencode"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved, nil
	}
	if resolved, err := resolveExecutableFromEnvPATH(env, "opencode"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved, nil
	}
	return "", fmt.Errorf("opencode executable not found")
}

func resolveOpenCodeBinaryCandidate(env []string, value string) (string, error) {
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
