package daemon

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type stagingCleanupCurrentGateway struct {
	recordingGateway
	base string
}

func (g *stagingCleanupCurrentGateway) PrivateStagingBase(gatewayID, appID string) (string, error) {
	if gatewayID != "current" || appID != "current-app" {
		return "", fmt.Errorf("private staging gateway unavailable")
	}
	return g.base, nil
}

func TestPrivateStagingCleanupSweepsPastUnavailableGateways(t *testing.T) {
	ctx := t.Context()
	base := t.TempDir()
	stage := func(gateway, event string) (string, string) {
		t.Helper()
		owner := privatestaging.Owner{Tenant: "tenant", AppID: gateway + "-app", Gateway: gateway, EventID: event, MessageID: event}
		store, err := privatestaging.Open(base, owner)
		if err != nil {
			t.Fatal(err)
		}
		path, err := store.Create(strings.NewReader(event), ".png")
		if err != nil {
			t.Fatal(err)
		}
		hash, err := privatestaging.ExpectedHash(base, owner, path)
		if err != nil || hash == "" {
			t.Fatalf("staged hash %q: %v", hash, err)
		}
		return path, hash
	}
	oldPath, oldHash := stage("expired", "expired-001")
	receiptPath, receiptHash := stage("current", "receipt")
	canceledPath, _ := stage("current", "canceled")
	dbPath := filepath.Join(t.TempDir(), "private-chat.db")
	j, err := privatechat.OpenJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	owner := privatechat.Owner{GatewayID: "current", AppID: "current-app", TenantID: "tenant", SurfaceID: "s", ChatID: "c", UserID: "u"}
	admit := func(event, lane, payload string) int64 {
		t.Helper()
		seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: event, Lane: lane, Payload: []byte(payload)})
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}
	receiptSeq := admit("receipt", "input", fmt.Sprintf(`{"MessageID":"receipt","LocalPath":%q}`, receiptPath))
	canceledSeq := admit("canceled", "input", fmt.Sprintf(`{"MessageID":"canceled","LocalPath":%q}`, canceledPath))
	controlSeq := admit("control", "control", `{}`)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	// Seed the durable results of receipt preparation and cancellation before
	// restarting the cleanup consumer. The expired gateway has no active config.
	func() {
		db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		body, err := privatechat.EncodeEffectEvent(eventcontract.Event{DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateStageInput, StageObjectID: "stage"}})
		if err != nil {
			t.Fatal(err)
		}
		exec := func(query string, args ...any) {
			t.Helper()
			if _, err := db.ExecContext(ctx, query, args...); err != nil {
				t.Fatal(err)
			}
		}
		exec(`WITH RECURSIVE backlog(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM backlog WHERE n<129)
INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state)
SELECT printf('expired-%03d',n),'tenant','expired-app','expired','s','c','u',1,n,0,'background',?,'confirmed' FROM backlog`, body)
		exec(`INSERT INTO private_staging_cleanup(effect_id,path,event_id,message_id,sha256,state)
SELECT id,CASE WHEN revision=1 THEN ? ELSE '/historical/'||id END,id,id,?,'pending' FROM private_effect`, oldPath, oldHash)
		exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state)
VALUES('receipt','tenant','current-app','current','s','c','u',1,130,0,'background',?,'confirmed')`, body)
		exec(`INSERT INTO private_staging_cleanup(effect_id,path,event_id,message_id,sha256,state)
VALUES('receipt',?,'receipt','receipt',?,'pending')`, receiptPath, receiptHash)
		exec(`INSERT INTO private_applied(seq,revision,canceled_by) VALUES(?,130,NULL),(?,0,?)`, receiptSeq, canceledSeq, controlSeq)
		exec(`INSERT INTO private_canceled_staging_cleanup(ingress_seq,path,message_id,state) VALUES(?,?,'canceled','pending')`, canceledSeq, canceledPath)
	}()

	j, err = privatechat.OpenJournal(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	host := New(":0", ":0", &stagingCleanupCurrentGateway{base: base}, agentproto.ServerIdentity{})
	for i := 0; i < 2; i++ {
		if err := runPrivateStagingCleanup(ctx, host, j); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{receiptPath, canceledPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("current staged file still present at %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != "expired-001" {
		t.Errorf("staged file for the unavailable gateway changed: %q %v", data, err)
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var receiptsPending, receiptsDone, canceledDone int
	err = db.QueryRowContext(ctx, `SELECT
(SELECT count(*) FROM private_staging_cleanup WHERE state='pending' AND effect_id LIKE 'expired-%'),
(SELECT count(*) FROM private_staging_cleanup WHERE state='done' AND effect_id='receipt'),
(SELECT count(*) FROM private_canceled_staging_cleanup WHERE state='done' AND ingress_seq=?)`, canceledSeq).Scan(&receiptsPending, &receiptsDone, &canceledDone)
	if err != nil || receiptsPending != 129 || receiptsDone != 1 || canceledDone != 1 {
		t.Fatalf("durable cleanup state: old pending=%d receipt done=%d canceled done=%d err=%v", receiptsPending, receiptsDone, canceledDone, err)
	}
}
