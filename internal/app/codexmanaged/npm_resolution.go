package codexmanaged

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// npmCommand runs the same npm CLI and environment for discovery and install.
func npmCommand(ctx context.Context, env []string, dir string, args ...string) ([]byte, error) {
	binary := "npm"
	if runtime.GOOS == "windows" {
		npm, err := executableFromEnv("npm", env)
		if err != nil {
			return nil, err
		}
		cli := filepath.Join(filepath.Dir(npm), "node_modules", "npm", "bin", "npm-cli.js")
		binary = "node"
		args = append([]string{cli}, args...)
	}
	binary, err := executableFromEnv(binary, env)
	if err != nil {
		return nil, err
	}
	cmd := execlaunch.CommandContext(ctx, binary, args...)
	cmd.Env = append([]string{}, env...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, npmFailure(err, out.Bytes())
	}
	return out.Bytes(), nil
}

// Only policy keys enter the digest; neither credentials nor configuration
// contents are logged or persisted. npm resolves precedence including .npmrc.
func npmPolicyFingerprint(ctx context.Context, env []string) (string, error) {
	staging, err := os.MkdirTemp("", "codex-npm-policy-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	raw, err := npmCommand(ctx, env, staging, "config", "list", "--json")
	if err != nil {
		return "", err
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("read npm policy: %w", err)
	}
	selected := map[string]json.RawMessage{}
	for _, key := range []string{"before", "min-release-age", "min-release-age-exclude", "tag", "omit", "include", "ignore-scripts", "engine-strict", "os", "cpu", "libc"} {
		if value, ok := cfg[key]; ok {
			selected[key] = value
		}
	}
	data, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func resolveLatestWithNPM(ctx context.Context, env []string) (string, error) {
	return resolveLatestWithNPMRegistry(ctx, env, "https://registry.npmjs.org")
}

func resolveLatestWithNPMRegistry(ctx context.Context, env []string, registry string) (string, error) {
	staging, err := os.MkdirTemp("", "codex-npm-resolve-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	_, err = npmCommand(ctx, env, staging, "install", "--prefix", staging, "--package-lock-only", "--ignore-scripts", "--no-audit", "--no-fund", "--registry="+registry, "--json", "@openai/codex@latest")
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(staging, "package-lock.json"))
	if err != nil {
		return "", fmt.Errorf("read npm resolution lock: %w", err)
	}
	var result struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("invalid npm resolution lock: %w", err)
	}
	version := strings.TrimSpace(result.Packages["node_modules/@openai/codex"].Version)
	if version == "" {
		return "", fmt.Errorf("npm resolution omitted Codex package")
	}
	return version, nil
}

// npm error prose can contain credentials; expose only its validated error code.
func npmFailure(cause error, raw []byte) error {
	start := bytes.IndexByte(raw, '{')
	if start >= 0 {
		var result struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(raw[start:], &result) == nil {
			code := result.Error.Code
			if len(code) > 0 && len(code) <= 40 {
				valid := true
				for _, ch := range code {
					if !(ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_') {
						valid = false
						break
					}
				}
				if valid {
					return fmt.Errorf("npm %s: %w", code, cause)
				}
			}
		}
	}
	return fmt.Errorf("npm failed: %w", cause)
}
