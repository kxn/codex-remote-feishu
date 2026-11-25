package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// sdkJourneyUser drives only the same message and card callbacks as a person.
// WaitVisible waits for the platform-facing operation rather than checkpoints.
type sdkJourneyUser struct {
	t       *testing.T
	ctx     context.Context
	gateway *messageIDAssigningGateway
	errors  <-chan error
	cwd     string
}

func (u sdkJourneyUser) send(app *App, id, text string) {
	u.t.Helper()
	v := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now(), EventID: id, MessageID: id, RawContent: fmt.Sprintf(`{"text":%q}`, text)}
	if err := app.applications.hook.Receive(u.ctx, v); err != nil {
		u.t.Fatal(err)
	}
}
func (u sdkJourneyUser) waitVisible(app *App, start int, text string) {
	u.t.Helper()
	for {
		ops := u.gateway.snapshotOperations()
		for _, op := range ops[start:] {
			body, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && strings.Contains(string(body), text) {
				return
			}
		}
		select {
		case e := <-u.errors:
			u.t.Fatal(e)
		case <-u.ctx.Done():
			for _, op := range u.gateway.snapshotOperations()[start:] {
				body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				u.t.Logf("visible-wait op=%s message=%s body=%s", op.Kind, op.MessageID, body)
			}
			if status, e := app.applications.processor.ReadRuntimeStatus(context.Background()); e == nil {
				for _, v := range status.Surfaces {
					u.t.Logf("diagnostic surface route=%s attached=%q pending=%v", v.RouteMode, v.AttachedInstanceID, v.PendingHeadless != nil)
				}
				for _, v := range status.Instances {
					u.t.Logf("diagnostic instance=%s online=%v cwd=%q", v.InstanceID, v.Online, v.WorkspaceRoot)
				}
			}
			u.t.Fatalf("visible text %q absent", text)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// A final belongs to its source message. Shared fixture answer text alone
// cannot distinguish this turn from a late update of a preceding turn.
func (u sdkJourneyUser) waitFinal(app *App, start int, source, text string) {
	u.t.Helper()
	for {
		for _, op := range u.gateway.snapshotOperations()[start:] {
			if op.ReplyToMessageID == source && strings.Contains(op.FinalSourceBody(), text) {
				return
			}
		}
		select {
		case err := <-u.errors:
			u.t.Fatal(err)
		case <-u.ctx.Done():
			for _, op := range u.gateway.snapshotOperations()[start:] {
				if body := op.FinalSourceBody(); body != "" {
					u.t.Logf("observed final source=%q message=%q body=%q", op.ReplyToMessageID, op.MessageID, body)
				}
			}
			u.t.Fatalf("final for source %q containing %q absent", source, text)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func (u sdkJourneyUser) chooseNew(app *App, prefix string) {
	u.t.Helper()
	before := len(u.gateway.snapshotOperations())
	u.send(app, prefix+"-list", "/list")
	var card feishu.Operation
	var doc any
	for card.MessageID == "" {
		for _, op := range u.gateway.snapshotOperations()[before:] {
			if op.Kind != feishu.OperationSendCard || op.MessageID == "" {
				continue
			}
			f, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && json.Unmarshal(f, &doc) == nil && findPrivateCallback(doc, "target_picker_confirm") != nil {
				card = op
				break
			}
		}
		if card.MessageID != "" {
			break
		}
		select {
		case e := <-u.errors:
			u.t.Fatal(e)
		case <-u.ctx.Done():
			for _, op := range u.gateway.snapshotOperations()[before:] {
				u.t.Logf("picker missing phase=%s operation=%s title=%q body=%q", prefix, op.Kind, op.CardTitle, op.CardBody)
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
			if err == nil {
				rows, e := db.Query(`SELECT i.event_id,i.lane,COALESCE(a.revision,-1) FROM ingress i LEFT JOIN private_applied a ON a.seq=i.seq ORDER BY i.seq DESC LIMIT 6`)
				if e == nil {
					for rows.Next() {
						var id, lane string
						var revision int64
						if rows.Scan(&id, &lane, &revision) == nil {
							u.t.Logf("ingress event=%s lane=%s applied_revision=%d", id, lane, revision)
						}
					}
					rows.Close()
				}
				rows, e = db.Query(`SELECT state,coalesce(json_extract(body,'$.event.Kind'),'') FROM private_effect ORDER BY rowid DESC LIMIT 8`)
				if e == nil {
					for rows.Next() {
						var state, kind string
						if rows.Scan(&state, &kind) == nil {
							u.t.Logf("effect state=%s kind=%s", state, kind)
						}
					}
					rows.Close()
				}
				db.Close()
			}
			u.t.Fatal("picker missing")
		case <-time.After(20 * time.Millisecond):
		}
	}
	f, e := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if e != nil {
		u.t.Fatal(e)
	}
	if e = json.Unmarshal(f, &doc); e != nil {
		u.t.Fatal(e)
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
	if value := findPrivateCallback(doc, "target_picker_select_workspace"); value != nil {
		click.EventID = prefix + "-workspace"
		click.Values = value
		click.Option = u.cwd
		response, e := app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
		if e != nil {
			u.t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &doc); e != nil {
			u.t.Fatal(e)
		}
	}
	click.EventID = prefix + "-confirm"
	click.Values = findPrivateCallback(doc, "target_picker_confirm")
	click.Option = ""
	if click.Values == nil {
		u.t.Fatal("confirm missing")
	}
	response, e := app.applications.hook.ReceiveInteractionResponse(u.ctx, click)
	if e != nil {
		u.t.Fatal(e)
	}
	if strings.Contains(string(response.RawBody), "已进入新会话待命") {
		return
	}
	for {
		for _, op := range u.gateway.snapshotOperations()[before:] {
			if op.MessageID != card.MessageID {
				continue
			}
			body, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if e == nil && strings.Contains(string(body), "已进入新会话待命") {
				return
			}
		}
		select {
		case e := <-u.errors:
			u.t.Fatal(e)
		case <-u.ctx.Done():
			for _, op := range u.gateway.snapshotOperations()[before:] {
				body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				u.t.Logf("picker phase=%s op=%s card=%s body=%s", prefix, op.Kind, op.MessageID, body)
			}
			u.t.Fatal("picker ready not visible")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
