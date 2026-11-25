package privatechat

import (
	"path/filepath"
	"testing"
)

func TestPendingStagingCleanupAfterSettlingCursorRows(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	owner := Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: "s", ChatID: "c", UserID: "u"}
	admit := func(event, lane string) int64 {
		t.Helper()
		seq, _, err := j.Admit(ctx, Ingress{Owner: owner, Generation: 1, EventID: event, Lane: lane, Payload: []byte(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}
	canceled := admit("canceled", "input")
	control := admit("control", "control")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := j.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO private_applied(seq,revision,canceled_by) VALUES(?,0,?)`, canceled, control)
	exec(`INSERT INTO private_effect(id,tenant,app_id,gateway,surface,chat,user_id,generation,revision,ordinal,lane,body,state)
VALUES('receipt','tenant','app','gateway','s','c','u',1,1,0,'background','{}','confirmed')`)
	exec(`INSERT INTO private_staging_cleanup(effect_id,path,event_id,message_id,sha256,state) VALUES
('receipt','/a','receipt','receipt-msg','sha','pending'),
('receipt','/b','receipt','receipt-msg','sha','pending'),
('receipt','/c','receipt','receipt-msg','sha','pending')`)
	exec(`INSERT INTO private_canceled_staging_cleanup(ingress_seq,path,message_id,state) VALUES
(?,'/a','cancel-msg','pending'),(?,'/b','cancel-msg','pending')`, canceled, canceled)

	page := func(after StagingCleanupPosition, want ...StagingCleanupPosition) []StagingCleanup {
		t.Helper()
		work, err := j.PendingStagingCleanupAfter(ctx, 2, after)
		if err != nil || len(work) != len(want) {
			t.Fatalf("page after %+v: %+v %v", after, work, err)
		}
		for i, pos := range want {
			if work[i].Position() != pos || work[i].Owner != owner || work[i].Generation != 1 {
				t.Fatalf("page after %+v item %d: %+v, want key %+v owner %+v", after, i, work[i], pos, owner)
			}
		}
		return work
	}
	settle := func(work ...StagingCleanup) {
		t.Helper()
		for _, item := range work {
			if err := j.FinishStagingCleanup(ctx, item, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	first := page(StagingCleanupPosition{}, StagingCleanupPosition{EffectID: "receipt", Path: "/a"}, StagingCleanupPosition{EffectID: "receipt", Path: "/b"})
	settle(first...)
	mixed := page(first[1].Position(), StagingCleanupPosition{EffectID: "receipt", Path: "/c"}, StagingCleanupPosition{IngressSequence: canceled, Path: "/a"})
	settle(mixed[1])
	last := page(mixed[1].Position(), StagingCleanupPosition{IngressSequence: canceled, Path: "/b"})
	settle(last...)
	page(last[0].Position())
	page(StagingCleanupPosition{}, mixed[0].Position())
}
