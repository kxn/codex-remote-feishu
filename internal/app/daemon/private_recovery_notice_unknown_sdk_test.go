package daemon

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

type recoveryNoticeUnknownGateway struct {
	*messageIDAssigningGateway
	mu        sync.Mutex
	attempts  int
	attempted chan struct{}
}

func (g *recoveryNoticeUnknownGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	for _, op := range ops {
		if strings.Contains(op.Text, "后台正在恢复，已收到这条消息，恢复完成后会自动执行。") {
			g.mu.Lock()
			g.attempts++
			if g.attempts == 1 {
				close(g.attempted)
			}
			g.mu.Unlock()
			return errors.New("controlled unknown recovery notice result")
		}
	}
	return g.messageIDAssigningGateway.Apply(ctx, ops)
}
func (g *recoveryNoticeUnknownGateway) wait(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-g.attempted:
	case <-ctx.Done():
		t.Fatal("recovery notice was not attempted")
	}
}
func (g *recoveryNoticeUnknownGateway) verify(t *testing.T, app *App) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.attempts != 1 {
		t.Fatalf("unknown notice attempts=%d", g.attempts)
	}
	for _, op := range g.snapshotOperations() {
		if strings.Contains(op.Text, "后台正在恢复，已收到这条消息，恢复完成后会自动执行。") {
			t.Fatal("unknown notice was recorded as successfully visible")
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var unknown int
	err = db.QueryRow(`SELECT count(*) FROM private_publication_ops WHERE state='unknown' AND json_extract(receipt,'$.Error')='controlled unknown recovery notice result' AND json_extract(body,'$.Operation.Text')=?`, "后台正在恢复，已收到这条消息，恢复完成后会自动执行。").Scan(&unknown)
	if err != nil || unknown != 1 {
		t.Fatalf("unknown notice evidence=%d err=%v", unknown, err)
	}
}
func TestPrivateSDKRecoveryNoticeUnknownDoesNotBlockInput(t *testing.T) {
	testPrivateSDKInterruptedShutdownRecovery(t, true, false, true, true, false, &recoveryNoticeUnknownGateway{attempted: make(chan struct{})})
}
