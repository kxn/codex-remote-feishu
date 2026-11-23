package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestPrivatePreviewRetrySurvivesRestartAndOnlyUpdatesConfirmedFinalCard(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "journal.db")
	o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "g", SurfaceID: "s", ChatID: "chat", UserID: "user"}
	seed := privatechat.New(nil, orchestrator.Config{})
	if err := seed.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	event := eventcontract.Event{Kind: eventcontract.KindBlockCommitted, SurfaceSessionID: o.SurfaceID, GatewayID: o.GatewayID, SourceMessageID: "original-user-input", Block: &render.Block{ID: "answer", Kind: render.BlockAssistantMarkdown, InstanceID: "instance", ThreadID: "thread", TurnID: "turn", ItemID: "item", Text: "See [report](report.md)", Final: true}}
	raw, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	gateway := &messageIDAssigningGateway{}
	preview := &secondChancePreviewer{secondText: "See [report](https://example.test/preview/report)"}
	open := func() (*privatechat.Journal, *privatePublisher) {
		j, e := privatechat.OpenJournal(path)
		if e != nil {
			t.Fatal(e)
		}
		host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
		host.finalBlockPreviewer = preview
		host.finalPreviewTimeout = 5 * time.Millisecond
		app := privatechat.New(nil, orchestrator.Config{})
		host.service = app.Service()
		p, e := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
		if e != nil {
			t.Fatal(e)
		}
		return j, &privatePublisher{host: host, processor: p, journal: j}
	}
	j, p := open()
	if err = j.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []privatechat.Effect{{ID: "final", Lane: "interactive", Body: raw}}); err != nil {
		t.Fatal(err)
	}
	effects, err := p.Publish(ctx, o, 1, "final")
	if err != nil || len(effects) != 1 {
		t.Fatalf("retry effects=%+v err=%v", effects, err)
	}
	retryID := effects[0].Effect.ID
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard {
		t.Fatalf("final=%+v", ops)
	}
	message := ops[0].MessageID
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, p = open()
	defer j.Close()
	updates, err := runPrivatePreviewRetry(ctx, p.host, p.processor, j, o, 1, retryID)
	if err != nil || len(updates) != 1 {
		t.Fatalf("updates=%+v err=%v", updates, err)
	}
	if _, err = p.Publish(ctx, o, 1, updates[0].Effect.ID); err != nil {
		t.Fatal(err)
	}
	ops = gateway.snapshotOperations()
	if len(ops) != 2 || ops[1].Kind != feishu.OperationUpdateCard || ops[1].MessageID != message || !strings.Contains(ops[1].CardBody, "https://example.test/preview/report") {
		t.Fatalf("retry replaced wrong card: %+v", ops)
	}
	cp, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || len(cp.Core.RecentFinalCards) != 1 || cp.Core.RecentFinalCards[0].SourceMessageID != "original-user-input" {
		t.Fatalf("preview patch changed source input: err=%v", err)
	}
	preview.mu.Lock()
	calls := preview.calls
	preview.mu.Unlock()
	if calls != 2 {
		t.Fatalf("recursive preview attempts=%d", calls)
	}
	if _, err = runPrivatePreviewRetry(ctx, p.host, p.processor, j, o, 1, retryID); err != nil {
		t.Fatal(err)
	}
	if len(gateway.snapshotOperations()) != 2 {
		t.Fatal("receipt replay duplicated update")
	}
}
