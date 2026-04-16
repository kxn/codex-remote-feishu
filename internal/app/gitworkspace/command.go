package gitworkspace

import (
	"os"
	"os/exec"
)

// PrepareCommand applies the shared non-interactive Git process defaults used
// by both interactive workspace import and Cron repo materialization.
func PrepareCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	baseEnv := cmd.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = append(baseEnv,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
	configureGitImportCommand(cmd)
}
