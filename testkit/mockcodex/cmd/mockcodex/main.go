package main

import (
	"bufio"
	"flag"
	"log"
	"os"

	"github.com/kxn/codex-remote-feishu/testkit/mockcodex"
)

func main() {
	requireInitialize := flag.Bool("require-initialize", false, "require initialize/initialized handshake before other requests")
	noAutoComplete := flag.Bool("no-auto-complete", false, "keep turns active until interrupted or completed manually")
	exitAfterFinalOutput := flag.Bool("exit-after-final-output", false, "exit after final item output without sending turn/completed")
	exitAfterFinalOutputCode := flag.Int("exit-after-final-output-code", 0, "exit code used with --exit-after-final-output")
	exitAfterInterrupt := flag.Bool("exit-after-interrupt", false, "exit after interrupt ack without sending turn/completed")
	exitAfterInterruptCode := flag.Int("exit-after-interrupt-code", 0, "exit code used with --exit-after-interrupt")
	flag.Parse()

	engine := mockcodex.New()
	engine.RequireInitialize = *requireInitialize
	engine.AutoComplete = !*noAutoComplete
	engine.ExitAfterFinalOutput = *exitAfterFinalOutput
	engine.ExitAfterInterrupt = *exitAfterInterrupt
	engine.ExitAfterOutputCode = *exitAfterFinalOutputCode
	engine.ExitAfterInterruptCode = *exitAfterInterruptCode
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
		if shouldExit, code := engine.ConsumeScheduledExit(); shouldExit {
			os.Exit(code)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}
