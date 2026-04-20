package execlaunch

import (
	"context"
	"os/exec"
)

// Command creates an external command with the repository's shared launch defaults.
func Command(name string, args ...string) *exec.Cmd {
	return Prepare(exec.Command(name, args...))
}

// CommandContext creates an external command with the repository's shared launch defaults.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return Prepare(exec.CommandContext(ctx, name, args...))
}

// Prepare applies the repository's shared launch defaults to an existing command.
func Prepare(cmd *exec.Cmd) *exec.Cmd {
	if cmd == nil {
		return nil
	}
	preparePlatform(cmd)
	return cmd
}
