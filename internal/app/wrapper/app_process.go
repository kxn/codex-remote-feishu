package wrapper

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

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
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, err
	}
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		return nil, nil, nil, err
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		_ = stderr.Close()
		_ = stderrWriter.Close()
		return nil, nil, nil, err
	}
	// Keep the parent read ends independent from Cmd.Wait. When StdoutPipe is
	// used, Wait may close the pipe before stdoutLoop has drained the child's
	// final frames.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	return stdin, stdout, stderr, nil
}

func childEnvWithProxy(proxyEnv, args []string) []string {
	if parseBoolEnv(codexprofile.CodexRuntimeResolvedEnv) {
		return config.BuildCodexResolvedChildEnv(os.Environ(), proxyEnv, args)
	}
	return config.BuildCodexChildEnv(os.Environ(), proxyEnv, args)
}

func generateInstanceID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("inst-%s", hex.EncodeToString(bytes[:])), nil
}
