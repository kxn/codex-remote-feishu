package main

import (
	"os"

	"github.com/kxn/codex-remote-feishu/internal/app/vscodeshim"
)

func main() {
	os.Exit(vscodeshim.RunMain(os.Args[1:]))
}
