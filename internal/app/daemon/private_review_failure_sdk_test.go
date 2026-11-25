package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateSDKStrictReviewFailureRetryAndParent(t *testing.T) {
	for _, stage := range []string{"thread/start", "review/start"} {
		t.Run(stage, func(t *testing.T) {
			cwd := t.TempDir()
			prepareRealReviewRepository(t, cwd)
			if err := os.WriteFile(filepath.Join(cwd, "review.txt"), []byte("changed"), 0600); err != nil {
				t.Fatal(err)
			}
			g := newGraphEnv(t, cwd, t.TempDir(), func(a *App, _ *messageIDAssigningGateway) {
				a.SetFinalBlockPreviewer(&workflowPreviewer{})
				a.headlessRuntime.BaseEnv = append(a.headlessRuntime.BaseEnv, "STRICT_REVIEW_FAIL="+stage)
			})
			t.Cleanup(func() { preserveReviewEvidenceOnFailure(t, g.app) })
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf 'codex-cli 0.159.3-dev.1\\n'; exit; fi\nPRIVATE_STRICT_REVIEW=1 exec %q -test.run '^TestPrivateStrictReviewNativeProcess$'\n", exe)
			if err = os.WriteFile(g.app.headlessRuntime.CodexRealBinary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			u := g.user
			n := len(u.gateway.snapshotOperations())
			u.send(g.app, "cold", "hi")
			u.waitVisible(g.app, n, "/list")
			u.chooseNew(g.app, "initial")
			n = len(u.gateway.snapshotOperations())
			u.send(g.app, "parent", "initial")
			u.waitFinal(g.app, n, "parent", "strict final")
			n = len(u.gateway.snapshotOperations())
			u.send(g.app, "fail-review", "/review uncommitted")
			for {
				if _, err := os.Stat(g.release + ".review-entered"); err == nil {
					break
				}
				select {
				case <-u.ctx.Done():
					t.Fatal("native review RPC did not reach controlled delay")
				case <-time.After(20 * time.Millisecond):
				}
			}
			statusBefore := len(u.gateway.snapshotOperations())
			u.send(g.app, "during-review-rpc", "/status")
			u.waitVisible(g.app, statusBefore, "当前状态")
			if err := os.WriteFile(g.release, []byte("release"), 0600); err != nil {
				t.Fatal(err)
			}
			u.waitVisible(g.app, n, "controlled review rejection")
			n = len(u.gateway.snapshotOperations())
			u.send(g.app, "after-failure", "ordinary after failure")
			u.waitFinal(g.app, n, "after-failure", "strict final")
			n = len(u.gateway.snapshotOperations())
			u.send(g.app, "retry-review", "/review uncommitted")
			u.waitFinal(g.app, n, "retry-review", "strict review final")
			trace, err := os.ReadFile(g.trace)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(trace), `"method":"review/start"`) < 1 {
				t.Fatal("review retry did not reach native")
			}
		})
	}
}
