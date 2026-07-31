package codexprofile

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type OAuthProbeOptions struct {
	BinaryPath string
	Env        []string
	Version    string
}

func RunOAuthProbe(ctx context.Context, options OAuthProbeOptions) (OAuthProbeObservation, error) {
	binaryPath := strings.TrimSpace(options.BinaryPath)
	if binaryPath == "" {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "launch_binary_missing"}
	}
	workDir, err := os.MkdirTemp("", "codex-remote-oauth-probe-*")
	if err != nil {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "launch_workdir"}
	}
	defer os.RemoveAll(workDir)
	material := OAuthProbeLaunchMaterial(options.Env)
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := execlaunch.CommandContext(childCtx, binaryPath, material.Args...)
	cmd.Dir = workDir
	cmd.Env = material.Env
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "launch_stdin"}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "launch_stdout"}
	}
	if err := cmd.Start(); err != nil {
		return OAuthProbeObservation{}, &OAuthProbeError{Code: ErrorOAuthProbeUnknown, Stage: "launch_start"}
	}

	observation, probeErr := RunOAuthProbeSession(childCtx, stdout, stdin, options.Version)
	_ = stdin.Close()
	cancel()
	_ = cmd.Wait()
	if probeErr != nil {
		return OAuthProbeObservation{}, probeErr
	}
	return observation, nil
}
