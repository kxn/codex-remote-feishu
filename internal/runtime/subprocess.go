package relayruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DetachedCommandOptions struct {
	BinaryPath string
	Args       []string
	Env        []string
	WorkDir    string
	StdoutPath string
	StderrPath string
}

func StartDetachedCommand(opts DetachedCommandOptions) (int, error) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	defer devNull.Close()

	stdout, stderr, err := detachedCommandOutputs(opts.StdoutPath, opts.StderrPath)
	if err != nil {
		return 0, err
	}
	if stdout != nil {
		defer stdout.Close()
	}
	if stderr != nil && stderr != stdout {
		defer stderr.Close()
	}

	binaryPath := strings.TrimSpace(opts.BinaryPath)
	if binaryPath == "" {
		return 0, os.ErrNotExist
	}
	binaryPath = filepath.Clean(binaryPath)
	cmd := exec.Command(binaryPath, opts.Args...)
	cmd.Stdin = devNull
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append([]string{}, opts.Env...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	prepareDetachedProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() {
		// Detached children still need a parent-side wait to avoid zombie buildup.
		_ = cmd.Wait()
	}()
	return cmd.Process.Pid, nil
}

func detachedCommandOutputs(stdoutPath, stderrPath string) (*os.File, *os.File, error) {
	stdoutPath = filepath.Clean(stdoutPath)
	stderrPath = filepath.Clean(stderrPath)
	if stdoutPath == "." || stdoutPath == "" {
		stdoutPath = os.DevNull
	}
	if stderrPath == "." || stderrPath == "" {
		stderrPath = stdoutPath
	}
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil && stdoutPath != os.DevNull {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(stderrPath), 0o755); err != nil && stderrPath != os.DevNull {
		return nil, nil, err
	}
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	if stderrPath == stdoutPath {
		return stdout, stdout, nil
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		stdout.Close()
		return nil, nil, err
	}
	return stdout, stderr, nil
}
