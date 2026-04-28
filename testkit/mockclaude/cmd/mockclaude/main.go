package main

import (
	"log"
	"os"

	"github.com/kxn/codex-remote-feishu/testkit/mockclaude"
)

func main() {
	if err := mockclaude.RunIO(mockclaude.NewFromEnv(), os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
