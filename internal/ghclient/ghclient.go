// Package ghclient provides a shared GitHub CLI client abstraction used by
// issuedocsync and issueworkflow.
package ghclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// Runner abstracts the gh CLI execution for testability.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ExecRunner executes the gh CLI via os/exec.
type ExecRunner struct{}

// Run executes "gh <args..." and returns combined output.
func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := execlaunch.CommandContext(ctx, "gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), message)
	}
	return output, nil
}
