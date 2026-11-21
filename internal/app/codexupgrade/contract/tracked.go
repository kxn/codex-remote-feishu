package contract

import "github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"

type TrackedInstallResult struct {
	Scope    hostinventory.Snapshot `json:"scope"`
	ExitCode int                    `json:"exit_code"`
}
