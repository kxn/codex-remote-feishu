package privatechat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPendingHostEffectsSkipsSettledNativeHistory(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("old-%d", i)
		if _, err = j.db.Exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES(?,'t','a','g','s','c','u',1,?,0,'background',CAST('{}' AS BLOB),'prepared')`, id, i+1); err != nil {
			t.Fatal(err)
		}
		if _, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES(?,'g','t','a','s','c','u',1,'inspect','resume','codex','n','thread','carrier',1,?,'accepted')`, id+":native", id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = j.db.Exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES('pending','t','a','g','s','c','u',1,201,0,'background',CAST('{}' AS BLOB),'unknown'),('confirmed','t','a','g','s','c','u',1,202,0,'background',CAST('{}' AS BLOB),'confirmed')`); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('pending:native','g','t','a','s','c','u',1,'inspect','resume','codex','n','thread','carrier',1,'p','unknown'),('confirmed:native','g','t','a','s','c','u',1,'control','resume','codex','n','thread','carrier',1,'c','failed')`); err != nil {
		t.Fatal(err)
	}
	work, err := j.PendingHostEffectsPage(t.Context(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 2 || work[0].Effect.Effect.ID != "pending" || work[1].Effect.Effect.ID != "confirmed" {
		t.Fatalf("settled history remained a candidate: %#v", work)
	}
}

func TestRunnableHostEffectsKeepsUncertainPublicationAndItsSuccessorsDormant(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	owner := Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "t", AppID: "a"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: owner.SurfaceID, Notice: &control.Notice{Text: "publication"}})
	if err != nil {
		t.Fatal(err)
	}
	old := Effect{ID: "old", Lane: "interactive", Body: body, DisplayKey: "display", OrderKeys: []string{"causal"}}
	effects := []Effect{old}
	for i := range 14 {
		held := Effect{ID: fmt.Sprintf("held-%d", i), Lane: "interactive", Body: body}
		if i%2 == 0 {
			held.DisplayKey = "display"
		} else {
			held.OrderKeys = []string{"causal"}
		}
		effects = append(effects, held)
	}
	effects = append(effects, Effect{ID: "current", Lane: "interactive", Body: body}, Effect{ID: "resume", Lane: "interactive", Body: body})
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: owner, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old.ID, "resume"} {
		if err = j.PreparePublication(ctx, owner, 1, id, json.RawMessage(`{}`), []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = j.db.ExecContext(ctx, `UPDATE private_effect SET state='unknown' WHERE id IN ('old','resume');
		UPDATE private_publication_ops SET state='unknown' WHERE effect_id='old' AND ordinal=0;
		UPDATE private_publication_ops SET state='confirmed' WHERE effect_id='resume' AND ordinal=0;`); err != nil {
		t.Fatal(err)
	}
	assertRunnable := func(want ...string) {
		t.Helper()
		rows, e := j.RunnableHostEffectsAfter(ctx, 128, HostEffectPosition{})
		if e != nil {
			t.Fatal(e)
		}
		var ids []string
		for _, row := range rows {
			ids = append(ids, row.Position().ID)
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("runnable effects = %v, want %v", ids, want)
		}
	}
	assertRunnable("current", "resume")
	record, err := j.LoadEffect(ctx, owner, 1, old.ID)
	if err != nil || record.State != "unknown" {
		t.Fatalf("unknown publication evidence missing: %v %v", record, err)
	}
	all, err := j.PendingHostEffectsAfter(ctx, 128, HostEffectPosition{})
	if err != nil || len(all) != len(effects) {
		t.Fatalf("audit lost pending records: %d %v", len(all), err)
	}
	// Model receipt reconciliation for the exact unknown operation. Its
	// confirmed prefix and prepared tail can continue through the publisher.
	if _, err = j.db.ExecContext(ctx, `UPDATE private_publication_ops SET state='confirmed' WHERE effect_id='old' AND ordinal=0`); err != nil {
		t.Fatal(err)
	}
	assertRunnable("old", "current", "resume")
}

func TestRunnableHostEffectsOmitsUndispatchedDetachedCarrierCommand(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_checkpoint(tenant,app_id,gateway,surface,chat,user_id,generation,revision,cursor,data,digest)
		VALUES('t','a','g','s','c','u',1,1,0,CAST('{"DetachedExecutions":[{"InstanceID":"old-carrier"}]}' AS BLOB),'test');
		INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES
		('old','t','a','g','s','c','u',1,1,0,'background',CAST('{"version":1,"event":{"Kind":"agent.command","Command":{"Kind":"prompt.send"}},"target_instance_id":"old-carrier"}' AS BLOB),'prepared'),
		('current','t','a','g','s','c','u',1,1,1,'background',CAST('{"version":1,"event":{"Kind":"agent.command","Command":{"Kind":"prompt.send"}},"target_instance_id":"new-carrier"}' AS BLOB),'prepared'),
		('receipt','t','a','g','s','c','u',1,1,2,'background',CAST('{"version":1,"event":{"Kind":"agent.command","Command":{"Kind":"prompt.send"}},"target_instance_id":"old-carrier"}' AS BLOB),'confirmed'),
		('legacy','t','a','g','s','c','u',1,1,3,'background',CAST('{"version":1,"event":{"Kind":"agent.command","Command":{"Kind":"prompt.send"}}}' AS BLOB),'prepared'),
		('invalid','t','a','g','s','c','u',1,1,4,'background',CAST('{' AS BLOB),'prepared')`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := j.RunnableHostEffectsAfter(ctx, 128, HostEffectPosition{})
	if err != nil || len(rows) != 3 || rows[0].Position().ID != "current" || rows[1].Position().ID != "receipt" || rows[2].Position().ID != "invalid" {
		t.Fatalf("wrong detached carrier candidates: %v %v", rows, err)
	}
	all, err := j.PendingHostEffectsAfter(ctx, 128, HostEffectPosition{})
	if err != nil || len(all) != 5 || all[0].Position().ID != "old" || all[3].Position().ID != "legacy" {
		t.Fatalf("detached command was lost from durable inventory: %v %v", all, err)
	}
}

func TestPendingHostEffectsAfterUsesIDToBreakTiesWhenCursorEffectSettles(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state) VALUES
		('first','t','a','g','surface-1','c1','u1',1,1,0,'interactive',CAST('{}' AS BLOB),'confirmed'),
		('next','t','a','g','surface-2','c2','u2',1,1,0,'interactive',CAST('{}' AS BLOB),'prepared')`)
	if err != nil {
		t.Fatal(err)
	}
	first, err := j.PendingHostEffectsAfter(ctx, 1, HostEffectPosition{})
	if err != nil || len(first) != 1 || first[0].Position().ID != "first" {
		t.Fatalf("first page: %v %v", first, err)
	}
	if _, err = j.db.ExecContext(ctx, `INSERT INTO private_effect_applied(effect_id,revision,receipt_digest) VALUES('first',1,'test')`); err != nil {
		t.Fatal(err)
	}
	next, err := j.PendingHostEffectsAfter(ctx, 1, first[0].Position())
	if err != nil || len(next) != 1 || next[0].Position().ID != "next" {
		t.Fatalf("candidate after settlement: %v %v", next, err)
	}
}
