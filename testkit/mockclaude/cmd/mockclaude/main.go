package main

import (
	"log"
	"os"

	"github.com/kxn/codex-remote-feishu/testkit/mockclaude"
)

func main() {
	if err := mockclaude.RunIO(mockclaude.NewFromEnvAndArgs(os.Args[1:]), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
