package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

const backlogUnknownResult = "controlled timeout writing old help card"

type privateBacklogGateway struct {
	*messageIDAssigningGateway
	oldWrites atomic.Int32
}

func (g *privateBacklogGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	for _, op := range ops {
		if op.Kind == feishu.OperationSendCard && op.CardTitle == "命令帮助" {
			g.oldWrites.Add(1)
			return errors.New(backlogUnknownResult)
		}
	}
	return g.messageIDAssigningGateway.Apply(ctx, ops)
}

func TestPrivateSDKPublicationBacklogKeepsDialogueAndMenuUsable(t *testing.T) {
	var gateway *privateBacklogGateway
	g := newGraphEnv(t, t.TempDir(), t.TempDir(), func(app *App, cards *messageIDAssigningGateway) {
		gateway = &privateBacklogGateway{messageIDAssigningGateway: cards}
		app.gateway = gateway
	})
	u := g.user
	u.chooseNew(g.app, "publication-backlog")

	db, err := sql.Open("sqlite", "file:"+filepath.Join(g.app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	unknownCount := func() int {
		t.Helper()
		var count int
		err := db.QueryRowContext(u.ctx, `SELECT count(*) FROM private_effect e
			JOIN private_publication_ops p ON p.effect_id=e.id
			WHERE e.state='unknown' AND p.state='unknown' AND json_extract(p.receipt,'$.Error')=?`, backlogUnknownResult).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	const oldCards = 14
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("old help writes attempted=%d", gateway.oldWrites.Load())
			for _, op := range gateway.snapshotOperations() {
				t.Logf("gateway operation=%s title=%q source=%q", op.Kind, op.CardTitle, op.ReplyToMessageID)
			}
		}
	})
	for i := 0; i < oldCards; i++ {
		u.send(g.app, fmt.Sprintf("old-help-%d", i), "/help")
	}
	// Start the new turn once the old uncertain writes occupy at least a
	// full publication pool. Further old attempts can finish independently.
	u.waitPublicationBacklog("eight durable uncertain writes", func() bool { return unknownCount() >= 8 })
	before := len(gateway.snapshotOperations())
	u.send(g.app, "backlog-hi", "hi")
	u.waitPublicationBacklog("first hi reaches Codex", func() bool {
		trace, _ := os.ReadFile(g.trace)
		return strings.Contains(string(trace), `"text":"hi"`)
	})
	u.waitBacklogTurn(before, "backlog-hi")

	before = len(gateway.snapshotOperations())
	u.send(g.app, "backlog-menu", "/menu")
	var cardID string
	var settings map[string]any
	u.waitPublicationBacklog("fresh private menu", func() bool {
		for _, op := range gateway.snapshotOperations()[before:] {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			body, err := feishu.FreezePrivateOperation(op, g.app.daemonLifecycleID)
			var document any
			if err == nil && json.Unmarshal(body, &document) == nil {
				if value := assembledMenuValue(document, "send_settings"); value != nil {
					cardID, settings = op.MessageID, value
					return true
				}
			}
		}
		return false
	})
	before = len(gateway.snapshotOperations())
	click := platformingress.HumanInteractionEnvelope{
		GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "backlog-menu-settings",
		SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: cardID,
		Kind: "card", Values: settings, ReceivedAt: time.Now(),
	}
	response, err := g.app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
	var settingsDocument any
	if err != nil || !strings.Contains(string(response.RawBody), "参数设置") || json.Unmarshal(response.RawBody, &settingsDocument) != nil || assembledMenuBack(settingsDocument) == nil {
		t.Fatalf("settings did not replace menu card %s: %s %v", cardID, response.RawBody, err)
	}
	for _, op := range gateway.snapshotOperations()[before:] {
		if op.Kind == feishu.OperationSendCard {
			t.Fatalf("menu settings appended a card: %q", op.CardTitle)
		}
	}
	u.send(g.app, "backlog-next-hi", "hi again")
	u.waitBacklogTurn(before, "backlog-next-hi")
	u.waitPublicationBacklog("all old failures remain uncertain", func() bool { return unknownCount() == oldCards })
	if got := gateway.oldWrites.Load(); got != oldCards {
		t.Fatalf("old uncertain writes attempted %d times for %d publications", got, oldCards)
	}
	trace, err := os.ReadFile(g.trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{`"text":"hi"`, `"text":"hi again"`} {
		if got := strings.Count(string(trace), prompt); got != 1 {
			t.Errorf("Codex received %s %d times", prompt, got)
		}
	}
}

func (u sdkJourneyUser) waitPublicationBacklog(label string, ready func() bool) {
	u.t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case err := <-u.errors:
			u.t.Fatal(err)
		case <-u.ctx.Done():
			u.t.Fatalf("%s: %v", label, u.ctx.Err())
		case <-deadline.C:
			u.t.Fatalf("missing %s", label)
		case <-tick.C:
		}
	}
}

func (u sdkJourneyUser) waitBacklogTurn(start int, source string) {
	u.t.Helper()
	u.waitPublicationBacklog("final reply and reaction receipts for "+source, func() bool {
		var final bool
		var added, removed string
		for _, op := range u.gateway.snapshotOperations()[start:] {
			if op.ReplyToMessageID == source && strings.Contains(op.FinalSourceBody(), "done-graph-a") {
				final = op.MessageID != ""
			}
			if op.MessageID != source || op.EmojiType != "THINKING" {
				continue
			}
			switch op.Kind {
			case feishu.OperationAddReaction:
				added = op.ReactionID
			case feishu.OperationRemoveReaction:
				removed = op.ReactionID
			}
		}
		return final && added != "" && removed == added
	})
}
