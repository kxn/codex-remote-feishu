package main

import (
	"os"

	"github.com/kxn/codex-remote-feishu/internal/app/launcher"
)

var version = "dev"
var branch = "dev"

func main() {
	os.Exit(launcher.Main(launcher.Options{
		Args:    []string{"daemon"},
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Version: version,
		Branch:  branch,
	}))
}
