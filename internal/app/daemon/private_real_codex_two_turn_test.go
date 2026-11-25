package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// This opt-in test uses the SDK hook and real wrapper/Codex, with a controlled
// platform gateway and an isolated copy of the native account configuration.
func testPrivateRealCodexTwoTurns(t *testing.T, ctx context.Context, r *applicationRuntime, gateway *messageIDAssigningGateway, app *App, raw platformingress.RawIngressEnvelope) {
	t.Helper()
	model := os.Getenv("PRIVATE_REAL_CODEX_MODEL")
	if model == "" {
		t.Fatal("set PRIVATE_REAL_CODEX_MODEL to a model listed by the account")
	}
	before := len(gateway.snapshotOperations())
	raw.EventID, raw.MessageID, raw.RawContent = "real-model-list", "real-model-list", `{"text":"/model"}`
	if err := r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	var value map[string]any
	for value == nil {
		for _, op := range gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if !strings.Contains(string(body), model) {
				continue
			}
			var doc any
			if json.Unmarshal(body, &doc) == nil {
				if hit := findPrivateModelSubmit(doc); hit != nil {
					card, value = op, hit
					break
				}
			}
		}
		if value == nil {
			select {
			case <-ctx.Done():
				t.Fatal("real native model catalog did not offer selected model")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "real-model-select", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, Form: map[string]any{"command_args_model_preset": model}, ReceivedAt: time.Now()}
	if _, err := r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	for {
		settings, err := app.readPrivateBotSettings("gateway", "app")
		if err == nil && settings.Record.PromptOverride.Model == model {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("model selection did not persist")
		case <-time.After(100 * time.Millisecond):
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	prompts := []string{"请只回复 BOREAL，不使用任何工具。", "上一条回复的英文单词是什么？只回复该单词，不使用任何工具。"}
	var session, previousTurn string
	for i, prompt := range prompts {
		before = len(gateway.snapshotOperations())
		raw.EventID = fmt.Sprintf("real-two-turn-%d", i+1)
		raw.MessageID = raw.EventID
		raw.RawContent = fmt.Sprintf(`{"text":%q}`, prompt)
		if err := r.hook.Receive(ctx, raw); err != nil {
			t.Fatalf("turn %d SDK admission: %v", i+1, err)
		}
		for {
			var command string
			e := db.QueryRowContext(ctx, `SELECT d.command_id FROM dispatch d JOIN dispatch_input di ON di.dispatch_id=d.id JOIN ingress i ON i.seq=di.seq WHERE i.event_id=?`, raw.EventID).Scan(&command)
			if e != nil && e != sql.ErrNoRows {
				t.Fatal(e)
			}
			if command != "" {
				var incarnation, thread, turn, status string
				e = db.QueryRowContext(ctx, `SELECT t.incarnation,json_extract(t.payload,'$.threadId'),json_extract(t.payload,'$.turnId'),json_extract(t.payload,'$.status') FROM native_events b JOIN native_events t ON t.incarnation=b.incarnation AND json_extract(t.payload,'$.threadId')=json_extract(b.payload,'$.threadId') AND json_extract(t.payload,'$.turnId')=json_extract(b.payload,'$.turnId') WHERE b.command_id=? AND json_extract(b.payload,'$.kind')='native.turn.binding.observed' AND json_extract(b.payload,'$.status')='accepted' AND json_extract(t.payload,'$.kind')='turn.completed' AND t.applied=1 ORDER BY t.sequence DESC LIMIT 1`, command).Scan(&incarnation, &thread, &turn, &status)
				if e != nil && e != sql.ErrNoRows {
					t.Fatal(e)
				}
				if e == nil {
					if status != "completed" {
						t.Fatalf("turn %d native terminal status=%s", i+1, status)
					}
					if thread == "" || turn == "" || session != "" && thread != session || previousTurn != "" && turn == previousTurn {
						t.Fatal("native session changed across turns")
					}
					var n int
					e = db.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND json_extract(payload,'$.kind')='item.completed' AND json_extract(payload,'$.itemKind')='agent_message' AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND upper(trim(json_extract(payload,'$.metadata.text')))='BOREAL' AND applied=1`, incarnation, thread, turn).Scan(&n)
					if e != nil {
						t.Fatal(e)
					}
					if n > 0 {
						var confirmed int
						e = db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect e JOIN private_publication_ops p ON p.effect_id=e.id WHERE e.state='confirmed' AND p.state='confirmed' AND upper(CAST(p.body AS TEXT)) LIKE '%BOREAL%' AND e.revision IN (SELECT applied_revision FROM native_events WHERE incarnation=? AND json_extract(payload,'$.threadId')=? AND json_extract(payload,'$.turnId')=? AND json_extract(payload,'$.kind') IN ('item.completed','turn.completed'))`, incarnation, thread, turn).Scan(&confirmed)
						if e != nil {
							t.Fatal(e)
						}
						displayed := false
						for _, op := range gateway.snapshotOperations()[before:] {
							body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
							if strings.Contains(strings.ToUpper(string(body)), "BOREAL") {
								displayed = true
								break
							}
						}
						if confirmed > 0 && displayed {
							session, previousTurn = thread, turn
							t.Logf("turn %d completed with confirmed assistant reply on same native session", i+1)
							break
						}
					}
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("turn %d did not complete exact native-to-platform chain: %v", i+1, ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}
