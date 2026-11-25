package daemon

import (
	"fmt"
	"os"
	"testing"
)

func TestPrivateSDKBlockingInputStopThenContinue(t *testing.T) {
	f := newContinuityFixtureBare(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_NONBLOCKING_PEER=1 PRIVATE_INPUT_BLOCKING=1 exec %q -test.run '^TestPrivateNonblockingInputNativePeer$'\n", exe)
	if err = os.WriteFile(f.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	u := f.user
	n := len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-cold", "hi")
	u.waitVisible(f.app, n, "/list")
	u.chooseNew(f.app, "blocking-new")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-establish", "establish")
	u.waitFinal(f.app, n, "blocking-establish", "input dialogue complete")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-first", "question")
	u.waitVisible(f.app, n, "Choose an option")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-denied", "continue")
	u.waitVisible(f.app, n, "待回答问题")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-stop", "/stop")
	u.waitVisible(f.app, n, "停止")
	n = len(f.gateway.snapshotOperations())
	u.send(f.app, "blocking-after", "continue after stop")
	u.waitFinal(f.app, n, "blocking-after", "input dialogue complete")
}
