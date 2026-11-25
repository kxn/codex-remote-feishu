package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// Only a temporary repository and a copied account configuration are used.
func prepareRealReviewRepository(t *testing.T, cwd string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", cwd)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %v %s", e, out)
	}
	if e := os.WriteFile(filepath.Join(cwd, "review.txt"), []byte("baseline\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"add", "review.txt"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "baseline"}} {
		cmd = exec.Command("git", args...)
		cmd.Dir = cwd
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git fixture: %v %s", e, out)
		}
	}
}
func testPrivateRealCodexReview(t *testing.T, ctx context.Context, r *applicationRuntime, gateway *messageIDAssigningGateway, app *App, raw platformingress.RawIngressEnvelope) {
	t.Helper()
	t.Cleanup(func() { preserveReviewEvidenceOnFailure(t, app) })
	g := graphEnv{app: app, user: sdkJourneyUser{t: t, ctx: ctx, gateway: gateway}, trace: ""}
	send := func(id, text string) {
		t.Helper()
		raw.EventID = id
		raw.MessageID = id
		raw.RawContent = fmt.Sprintf(`{"text":%q}`, text)
		raw.ReceivedAt = time.Now()
		if err := r.hook.Receive(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	waitFinal := func(from int, source string) {
		t.Helper()
		for {
			for _, op := range gateway.snapshotOperations()[from:] {
				if op.ReplyToMessageID == source && strings.TrimSpace(op.FinalSourceBody()) != "" {
					return
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("native final absent for %s", source)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	before := len(gateway.snapshotOperations())
	send("real-review-parent", "请将当前工作目录中 review.txt 的内容改为 changed，并在完成后简短回复。")
	waitFinal(before, "real-review-parent")
	beforeReview := len(gateway.snapshotOperations())
	card, _ := clickReviewAction(t, g, before, "surface.button.review_start", "real-review-click")
	before = beforeReview
	waitFinal(before, card)
	beforeFollow := len(gateway.snapshotOperations())
	cardStart := before
	if os.Getenv("PRIVATE_REAL_CODEX_REVIEW_FOLLOWUP") == "1" {
		_, response := clickReviewAction(t, g, before, "surface.button.review_follow_up", "real-review-follow-button")
		if !strings.Contains(string(response), "请发送一条文字追问") {
			g.user.waitVisible(app, beforeFollow, "请发送一条文字追问")
		}
		beforeFollow = len(gateway.snapshotOperations())
		send("real-review-follow", "请简要解释你发现的最重要问题。")
		waitFinal(beforeFollow, "real-review-follow")
		cardStart = beforeFollow
	}
	_, response := clickReviewAction(t, g, cardStart, "surface.button.review_discard", "real-review-exit")
	if !strings.Contains(string(response), "已退出审阅") {
		g.user.waitVisible(app, beforeFollow, "已退出审阅")
	}
	before = len(gateway.snapshotOperations())
	send("real-review-after", "请只回复 REVIEW_OK。")
	waitFinal(before, "real-review-after")
	follow := ""
	if os.Getenv("PRIVATE_REAL_CODEX_REVIEW_FOLLOWUP") == "1" {
		follow = "real-review-follow"
	}
	assertReviewNativeOwnership(t, ctx, app, "real-review-parent", "real-review-after", follow)
}

func assertReviewNativeOwnership(t *testing.T, ctx context.Context, app *App, first, last, followSource string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	lookup := func(source string) string {
		var session string
		err := db.QueryRowContext(ctx, `SELECT d.session FROM dispatch d JOIN dispatch_input di ON di.dispatch_id=d.id JOIN ingress i ON i.seq=di.seq WHERE i.event_id=? ORDER BY d.rowid DESC LIMIT 1`, source).Scan(&session)
		if err != nil {
			t.Fatalf("source %s dispatch: %v", source, err)
		}
		return session
	}
	parent, after := lookup(first), lookup(last)
	var observedParent, child string
	err = db.QueryRowContext(ctx, `SELECT json_extract(payload,'$.threadId'),json_extract(payload,'$.metadata.reviewThreadID') FROM native_events WHERE json_extract(payload,'$.action')='review.start' AND json_extract(payload,'$.status')='accepted' AND applied=1 ORDER BY sequence DESC LIMIT 1`).Scan(&observedParent, &child)
	if err != nil || parent == "" || child == "" || child == parent || observedParent != parent || after != parent {
		t.Fatalf("review parent/child evidence mismatch parent=%q after=%q observed=%q child=%q err=%v", parent, after, observedParent, child, err)
	}
	if followSource != "" {
		if follow := lookup(followSource); follow != child {
			t.Fatalf("review follow-up used session %q, expected child %q", follow, child)
		}
	}
	var backend, namespace string
	err = db.QueryRowContext(ctx, `SELECT d.backend,d.namespace FROM dispatch d JOIN dispatch_input di ON di.dispatch_id=d.id JOIN ingress i ON i.seq=di.seq WHERE i.event_id=? ORDER BY d.rowid DESC LIMIT 1`, first).Scan(&backend, &namespace)
	if err != nil {
		t.Fatal(err)
	}
	registry := app.applications.registry
	parentOwner, known, err := registry.SessionOwner(ctx, backend, namespace, parent)
	if err != nil || !known || parentOwner.Application != executionowner.Private {
		t.Fatalf("parent owner missing: %+v %t %v", parentOwner, known, err)
	}
	childOwner, known, err := registry.SessionOwner(ctx, backend, namespace, child)
	if err != nil || !known || childOwner != parentOwner {
		t.Fatalf("review child owner mismatch: %+v %t %v", childOwner, known, err)
	}
	foreign := parentOwner
	foreign.ID += "-foreign"
	known, allowed, err := registry.SessionAccess(ctx, backend, namespace, child, foreign)
	if err != nil || !known || allowed {
		t.Fatalf("foreign review access: known=%t allowed=%t err=%v", known, allowed, err)
	}
	t.Logf("review verified: parent=%s child=%s", parent, child)
}

func verifyRealReviewShutdown(t *testing.T, app *App, ownsChild func(int) bool, diagnostic io.Writer, stackPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-carriers.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	err = db.QueryRow(`SELECT record FROM carriers WHERE state='running' LIMIT 1`).Scan(&raw)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	var carrier privateCarrierRecord
	if err = json.Unmarshal(raw, &carrier); err != nil {
		t.Fatal(err)
	}
	if stackPath != "" {
		captureReviewProcessTree(t, carrier.Process.PID, filepath.Join(filepath.Dir(stackPath), "process-before.log"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if diagnostic != nil {
		fmt.Fprintf(diagnostic, "fixture shutdown started utc=%s pid=%d\n", time.Now().UTC().Format(time.RFC3339Nano), carrier.Process.PID)
	}
	if err = app.shutdownApplications(ctx); err != nil {
		if diagnostic != nil {
			fmt.Fprintf(diagnostic, "fixture shutdown returned utc=%s error=%v\n", time.Now().UTC().Format(time.RFC3339Nano), err)
		}
		t.Errorf("review carrier shutdown/inspection: %v", err)
		preserveReviewEvidenceOnFailure(t, app)
		if stackPath != "" {
			size := 1 << 20
			for {
				stack := make([]byte, size)
				n := runtime.Stack(stack, true)
				if n < len(stack) || size >= 64<<20 {
					if e := os.WriteFile(stackPath, stack[:n], 0600); e != nil {
						t.Logf("daemon stack artifact: %v", e)
					} else {
						t.Logf("isolated daemon goroutine artifact=%s", stackPath)
					}
					break
				}
				size *= 2
			}
		}
		pid := carrier.Process.PID
		current, identityErr := hostinventory.CaptureProcess(pid)
		if identityErr == nil && current == carrier.Process && ownsChild(pid) {
			fmt.Fprintf(os.Stderr, "isolated review SIGQUIT wrapper_pid=%d\n", pid)
			process, e := os.FindProcess(pid)
			if e == nil {
				e = process.Signal(syscall.Signal(3))
			}
			if e != nil {
				t.Logf("isolated SIGQUIT failed: %v", e)
			} else {
				time.Sleep(time.Second)
			}
		} else {
			t.Logf("isolated SIGQUIT skipped: identity or child ownership mismatch (pid=%d err=%v)", pid, identityErr)
		}
		t.FailNow()
	}
	db, err = sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-carriers.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var retired, unsettled int
	if err = db.QueryRow(`SELECT count(*) FROM carriers WHERE state='retired'`).Scan(&retired); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM carriers WHERE state IN ('running','draining','sealed')`).Scan(&unsettled); err != nil {
		t.Fatal(err)
	}
	if retired < 1 || unsettled != 0 {
		t.Fatalf("carrier retirement retired=%d unsettled=%d", retired, unsettled)
	}
}
