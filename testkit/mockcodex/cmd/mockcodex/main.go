package main

import (
	"bufio"
	"flag"
	"log"
	"os"

	"github.com/kxn/codex-remote-feishu/testkit/mockcodex"
)

func main() {
	requireInitialize := flag.Bool("require-initialize", false, "require initialize before other requests")
	flag.Parse()

	engine := mockcodex.New()
	engine.RequireInitialize = *requireInitialize
	engine.SeedThread("thread-1", "/data/dl/droid", "修复登录流程")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		outputs, err := engine.HandleRemoteCommand(append(scanner.Bytes(), '\n'))
		if err != nil {
			log.Printf("mockcodex: %v", err)
			continue
		}
		for _, output := range outputs {
			if _, err := os.Stdout.Write(output); err != nil {
				log.Fatal(err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}
