// Command shim is the unified shim binary. It dispatches on how it was
// invoked: an entrypoint whose name contains "upgrade" (or whose sidecar was
// written by an upgrade shim) runs the upgrade transaction helper; anything
// else runs the daemon-managed VS Code entrypoint shim.
package main

import (
	"os"

	"github.com/kxn/codex-remote-feishu/internal/app/upgradeshim"
	"github.com/kxn/codex-remote-feishu/internal/app/vscodeshim"
	"github.com/kxn/codex-remote-feishu/internal/shim"
)

func main() {
	mode := shim.DispatchMode(os.Args[0])
	switch mode {
	case shim.ModeUpgrade:
		os.Exit(upgradeshim.RunMain(os.Args[1:]))
	default:
		os.Exit(vscodeshim.RunMain(os.Args[1:]))
	}
}
