package codexmanaged

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func readBinaryVersion(ctx context.Context, binary string) (string, error) {
	return readBinaryVersionWithEnv(ctx, binary, os.Environ())
}

func readBinaryVersionWithEnv(ctx context.Context, binary string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output versionOutput
	binary, err := configuredCodexBinary(binary, env)
	if err != nil {
		return "", err
	}
	cmd := execlaunch.CommandContext(ctx, binary, "--version")
	cmd.Env = append([]string{}, env...)
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("read Codex binary version: %w", err)
	}
	fields := strings.Fields(output.String())
	if output.exceeded {
		return "", fmt.Errorf("Codex --version response exceeds limit")
	}
	if len(fields) != 2 || (fields[0] != "codex-cli" && fields[0] != "codex") {
		return "", fmt.Errorf("unrecognized Codex --version response")
	}
	return fields[1], nil
}

type versionOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (w *versionOutput) String() string { return w.buffer.String() }

func (w *versionOutput) Write(p []byte) (int, error) {
	remaining := 1024 - w.buffer.Len()
	if len(p) > remaining {
		w.exceeded = true
		_, _ = w.buffer.Write(p[:remaining])
	} else {
		_, _ = w.buffer.Write(p)
	}
	return len(p), nil
}

func installPrivate(ctx context.Context, staging, target string) error {
	return installPrivateWithEnv(ctx, staging, target, os.Environ())
}

func installPrivateWithEnv(ctx context.Context, staging, target string, env []string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// Explicit prefix and registry keep both install destination and release
	// source independent of user npm defaults. Auth and CODEX_HOME are inherited.
	args := []string{"install", "--prefix", staging, "--no-audit", "--no-fund", "--json", "--registry=https://registry.npmjs.org", "@openai/codex@" + target}
	binary := "npm"
	if runtime.GOOS == "windows" {
		npm, err := executableFromEnv("npm", env)
		if err != nil {
			return err
		}
		cli := filepath.Join(filepath.Dir(npm), "node_modules", "npm", "bin", "npm-cli.js")
		if _, err := os.Stat(cli); err != nil {
			return fmt.Errorf("resolve npm CLI script: %w", err)
		}
		binary = "node"
		args = append([]string{cli}, args...)
	}
	binary, err := executableFromEnv(binary, env)
	if err != nil {
		return err
	}
	cmd := execlaunch.CommandContext(ctx, binary, args...)
	cmd.Env = append([]string{}, env...)
	cmd.Dir = staging
	// npm output can contain credentials. Only accept a restricted structured error code.
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, io.Discard
	if err := cmd.Run(); err != nil {
		return npmFailure(err, output.Bytes())
	}
	return nil
}

// Use the native executable so Windows does not need a .cmd shell launcher.
// Current npm packages use vendor/<triple>/bin; older packages used /codex.
func installedBinary(prefix string) (string, error) {
	triple, platform, executable := platformPackage()
	if triple == "" {
		return "", fmt.Errorf("private Codex install unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return installedBinaryForPlatform(prefix, triple, platform, executable)
}

func installedBinaryForPlatform(prefix, triple, platform, executable string) (string, error) {
	modules := filepath.Join(prefix, "node_modules")
	packageRoot := filepath.Join(modules, "@openai", "codex")
	roots := []string{
		filepath.Join(modules, "@openai", platform, "vendor"),
		filepath.Join(packageRoot, "node_modules", "@openai", platform, "vendor"),
		filepath.Join(packageRoot, "vendor"),
	}
	for _, root := range roots {
		for _, directory := range []string{"bin", "codex"} {
			path := filepath.Join(root, triple, directory, executable)
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("Codex npm package missing native executable for %s", triple)
}

func platformPackage() (triple, platform, executable string) {
	arch, npmArch := "", ""
	switch runtime.GOARCH {
	case "amd64":
		arch, npmArch = "x86_64", "x64"
	case "arm64":
		arch, npmArch = "aarch64", "arm64"
	default:
		return "", "", ""
	}
	executable = "codex"
	switch runtime.GOOS {
	case "linux":
		return arch + "-unknown-linux-musl", "codex-linux-" + npmArch, executable
	case "darwin":
		return arch + "-apple-darwin", "codex-darwin-" + npmArch, executable
	case "windows":
		return arch + "-pc-windows-msvc", "codex-win32-" + npmArch, executable + ".exe"
	default:
		return "", "", ""
	}
}
