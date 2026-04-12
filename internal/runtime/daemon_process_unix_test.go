//go:build !windows

package relayruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartDetachedDaemonSurvivesParentExit(t *testing.T) {
	if os.Getenv("GO_WANT_DETACHED_HELPER") == "1" {
		helperRunDetached(t)
		return
	}

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "child.pid")
	logFile := filepath.Join(tempDir, "relayd.log")
	script := filepath.Join(tempDir, "child.sh")
	scriptBody := "#!/usr/bin/env bash\nset -euo pipefail\necho $$ > \"$PID_FILE\"\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	helper := exec.Command(os.Args[0], "-test.run=^TestStartDetachedDaemonSurvivesParentExit$")
	helper.Env = append(os.Environ(),
		"GO_WANT_DETACHED_HELPER=1",
		"DETACHED_HELPER_SCRIPT="+script,
		"DETACHED_HELPER_PID_FILE="+pidFile,
		"DETACHED_HELPER_LOG_FILE="+logFile,
	)
	output, err := helper.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, string(output))
	}

	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			rawText := strings.TrimSpace(string(raw))
			if _, scanErr := fmt.Sscanf(rawText, "%d", &pid); scanErr == nil && pid > 0 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("expected detached child pid to be recorded")
	}
	if !processAlive(pid) {
		t.Fatalf("expected detached child pid %d to survive parent exit", pid)
	}
	if err := terminateProcess(pid, time.Second); err != nil {
		t.Fatalf("terminate detached child %d: %v", pid, err)
	}
}

func TestStartDetachedDaemonPropagatesXDGEnvFromPaths(t *testing.T) {
	if os.Getenv("GO_WANT_DETACHED_ENV_HELPER") == "1" {
		helperRunDetachedEnv(t)
		return
	}

	tempDir := t.TempDir()
	outputFile := filepath.Join(tempDir, "env.txt")
	script := filepath.Join(tempDir, "child.sh")
	scriptBody := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n%s\\n%s\\n' \"$XDG_CONFIG_HOME\" \"$XDG_DATA_HOME\" \"$XDG_STATE_HOME\" > \"$ENV_OUT\"\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	helper := exec.Command(os.Args[0], "-test.run=^TestStartDetachedDaemonPropagatesXDGEnvFromPaths$")
	helper.Env = append(os.Environ(),
		"GO_WANT_DETACHED_ENV_HELPER=1",
		"DETACHED_ENV_HELPER_SCRIPT="+script,
		"DETACHED_ENV_OUTPUT="+outputFile,
	)
	output, err := helper.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, string(output))
	}

	var got string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(outputFile)
		if err == nil {
			got = string(raw)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got == "" {
		t.Fatal("expected detached child env output")
	}
	want := strings.Join([]string{
		filepath.Join(tempDir, "cfg-home"),
		filepath.Join(tempDir, "data-home"),
		filepath.Join(tempDir, "state-home"),
		"",
	}, "\n")
	if got != want {
		t.Fatalf("env output = %q, want %q", got, want)
	}
}

func TestStartDetachedDaemonReapsExitedChild(t *testing.T) {
	tempDir := t.TempDir()
	script := filepath.Join(tempDir, "child.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	pid, err := StartDetachedDaemon(LaunchOptions{
		BinaryPath: script,
		Paths: Paths{
			StateDir:      tempDir,
			LogsDir:       tempDir,
			DaemonLogFile: filepath.Join(tempDir, "daemon.log"),
		},
	})
	if err != nil {
		t.Fatalf("StartDetachedDaemon: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected detached child pid %d to be reaped after exit", pid)
}

func TestTerminateProcessReapsZombieChildImmediately(t *testing.T) {
	tempDir := t.TempDir()
	script := filepath.Join(tempDir, "child.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	cmd := exec.Command(script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	startedAt := time.Now()
	if err := terminateProcess(cmd.Process.Pid, 500*time.Millisecond); err != nil {
		t.Fatalf("terminateProcess: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
		t.Fatalf("terminateProcess took %s for already-exited child, want under 250ms", elapsed)
	}
	if processAlive(cmd.Process.Pid) {
		t.Fatalf("expected zombie child pid %d to be reaped", cmd.Process.Pid)
	}
}

func helperRunDetached(t *testing.T) {
	t.Helper()
	script := os.Getenv("DETACHED_HELPER_SCRIPT")
	pidFile := os.Getenv("DETACHED_HELPER_PID_FILE")
	logFile := os.Getenv("DETACHED_HELPER_LOG_FILE")
	if script == "" || pidFile == "" || logFile == "" {
		t.Fatal("missing detached helper env")
	}
	paths := Paths{
		StateDir:      filepath.Dir(logFile),
		LogsDir:       filepath.Dir(logFile),
		DaemonLogFile: logFile,
	}
	_, err := StartDetachedDaemon(LaunchOptions{
		BinaryPath: script,
		Env:        append(os.Environ(), "PID_FILE="+pidFile),
		Paths:      paths,
	})
	if err != nil {
		t.Fatalf("StartDetachedDaemon: %v", err)
	}
}

func helperRunDetachedEnv(t *testing.T) {
	t.Helper()
	script := os.Getenv("DETACHED_ENV_HELPER_SCRIPT")
	outputFile := os.Getenv("DETACHED_ENV_OUTPUT")
	if script == "" || outputFile == "" {
		t.Fatal("missing detached env helper env")
	}
	tempDir := filepath.Dir(outputFile)
	paths := Paths{
		ConfigDir: filepath.Join(tempDir, "cfg-home", ProductName),
		DataDir:   filepath.Join(tempDir, "data-home", ProductName),
		StateDir:  filepath.Join(tempDir, "state-home", ProductName),
	}
	_, err := StartDetachedDaemon(LaunchOptions{
		BinaryPath: script,
		Env:        append(os.Environ(), "ENV_OUT="+outputFile),
		Paths:      paths,
	})
	if err != nil {
		t.Fatalf("StartDetachedDaemon: %v", err)
	}
}
