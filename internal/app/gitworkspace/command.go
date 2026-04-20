package gitworkspace

import (
	"os"
	"os/exec"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// PrepareCommand applies the shared non-interactive Git process defaults used
// by both interactive workspace import and Cron repo materialization.
func PrepareCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	execlaunch.Prepare(cmd)
	baseEnv := cmd.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = append(baseEnv,
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
}
