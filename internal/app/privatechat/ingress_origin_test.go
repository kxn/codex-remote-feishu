package privatechat

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestOriginOpenExistingSchemaFixture(t *testing.T) {
	path := os.Getenv("PRIVATE_ORIGIN_SCHEMA_FIXTURE")
	if path == "" {
		t.Skip("no copied fixture")
	}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var version, columns int
	if err = j.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 9 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	if err = j.db.QueryRow(`SELECT count(*) FROM pragma_table_info('ingress') WHERE name IN ('origin_router','origin_seq')`).Scan(&columns); err != nil || columns != 2 {
		t.Fatalf("origin columns=%d err=%v", columns, err)
	}
}

func TestOriginDiscardBothAdmissionOrdersAndEpoch(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "admitted-first", true: "admitted-late"}[late], func(t *testing.T) {
			ctx := context.Background()
			j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			o := Owner{"g", "s", "c", "u", "tenant", "app"}
			input := Ingress{Owner: o, Generation: 1, EventID: "old-image", Lane: "input", Payload: []byte(`{"MessageID":"image-message","LocalPath":"/tmp/managed-image"}`), OriginRouter: "router", OriginSequence: 10}
			control := Ingress{Owner: o, Generation: 1, EventID: "new", Lane: "control", Payload: []byte(`{}`), OriginRouter: "router", OriginSequence: 11}
			var inputSeq int64
			if !late {
				inputSeq, _, err = j.Admit(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
			}
			controlSeq, _, err := j.Admit(ctx, control)
			if err != nil {
				t.Fatal(err)
			}
			other := input
			other.EventID = "other-epoch"
			other.Generation = 2
			other.OriginSequence = 9
			otherSeq, _, err := j.Admit(ctx, other)
			if err != nil {
				t.Fatal(err)
			}
			foreignRouter := input
			foreignRouter.EventID = "other-router"
			foreignRouter.OriginRouter = "other-router"
			foreignRouter.OriginSequence = 8
			foreignSeq, _, err := j.Admit(ctx, foreignRouter)
			if err != nil {
				t.Fatal(err)
			}
			foreignOwner := input
			foreignOwner.EventID = "other-owner"
			foreignOwner.Owner.UserID = "other-user"
			foreignOwner.Owner.ChatID = "other-chat"
			foreignOwner.Owner.SurfaceID = "other-surface"
			foreignOwnerSeq, _, err := j.Admit(ctx, foreignOwner)
			if err != nil {
				t.Fatal(err)
			}
			seed := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
			seed.MaterializeSurface(o.SurfaceID, o.GatewayID, o.ChatID, o.UserID)
			core, err := seed.ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cp := Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core, cancelEarlierInputBefore: controlSeq}
			if err = j.commitCheckpoint(ctx, cp, 0, nil, controlSeq, nil); err != nil {
				t.Fatal(err)
			}
			if late {
				inputSeq, _, err = j.Admit(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
			}
			var by int64
			if err = j.db.QueryRow(`SELECT canceled_by FROM private_applied WHERE seq=?`, inputSeq).Scan(&by); err != nil || by != controlSeq {
				t.Fatalf("cancel=%d want=%d err=%v", by, controlSeq, err)
			}
			var n int
			if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, otherSeq).Scan(&n); err != nil || n != 0 {
				t.Fatalf("other epoch consumed: %d %v", n, err)
			}
			if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, foreignSeq).Scan(&n); err != nil || n != 0 {
				t.Fatalf("other router consumed: %d %v", n, err)
			}
			if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, foreignOwnerSeq).Scan(&n); err != nil || n != 0 {
				t.Fatalf("other owner consumed: %d %v", n, err)
			}
			work, err := j.PendingStagingCleanup(ctx, 10)
			if err != nil || len(work) != 1 || work[0].IngressSequence != inputSeq || work[0].Path != "/tmp/managed-image" {
				t.Fatalf("cleanup: %+v %v", work, err)
			}
			path := filepath.Join(filepath.Dir(j.lock.Name()), "journal.db")
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			j, err = OpenJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			work, err = j.PendingStagingCleanup(ctx, 10)
			if err != nil || len(work) != 1 {
				t.Fatalf("cleanup lost after restart: %+v %v", work, err)
			}
			if err = j.FinishStagingCleanup(ctx, work[0], true); err != nil {
				t.Fatal(err)
			}
			work, err = j.PendingStagingCleanup(ctx, 10)
			if err != nil || len(work) != 0 {
				t.Fatalf("cleanup not idempotently settled: %+v %v", work, err)
			}
		})
	}
}
