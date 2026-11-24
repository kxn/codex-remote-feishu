package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRecoveryGateChecksEachQueuedSourceForMigratedOwner(t *testing.T) {
	for _, mode := range []string{"new input", "claimed source", "unknown dispatch", "accepted dispatch", "known unsent"} {
		t.Run(mode, func(t *testing.T) {
			held := mode == "claimed source" || mode == "unknown dispatch" || mode == "accepted dispatch"
			ctx := context.Background()
			j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			o := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
			seq, _, err := j.Admit(ctx, Ingress{Owner: o, Generation: 1, EventID: "event", Lane: "input", Payload: []byte(`{}`), Sources: []string{"message"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.Exec(`INSERT INTO private_applied(seq,revision) VALUES(?,1)`, seq); err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.Exec(`INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('import','source','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.Exec(`INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('import','old','old-message','claimed','old-hash')`); err != nil {
				t.Fatal(err)
			}
			if mode == "claimed source" {
				if _, err = j.db.Exec(`INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('import','inbox','message','claimed','hash')`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unknown dispatch" || mode == "accepted dispatch" || mode == "known unsent" {
				status := "failed"
				if mode == "unknown dispatch" {
					status = "unknown"
				}
				if mode == "accepted dispatch" {
					status = "accepted"
				}
				_, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('d',?,?,?,?,?,?,1,'input','start','codex','ns','session','carrier',1,'command',?)`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, status)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = j.db.Exec(`INSERT INTO dispatch_input(dispatch_id,seq) VALUES('d',?)`, seq); err != nil {
					t.Fatal(err)
				}
			}
			seed := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
			seed.MaterializeSurface(o.SurfaceID, o.GatewayID, o.ChatID, o.UserID)
			cp, err := seed.ExportPrivateCheckpoint(o.SurfaceID)
			if err != nil {
				t.Fatal(err)
			}
			cp.NextQueueID = 1
			cp.QueuedQueueItemIDs = []string{"queue-1"}
			cp.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: o.SurfaceID, ActorUserID: o.UserID, SourceKind: state.QueueItemSourceUser, SourceMessageID: "message", SourceMessageIDs: []string{"message"}, Status: state.QueueItemQueued}}
			app := New(nil, orchestrator.Config{})
			if err = app.Service().RestorePrivateCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			p, err := NewProcessor(app, j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.reconcileRecoveryGate(ctx, o, 1); err != nil {
				t.Fatal(err)
			}
			allowed := app.Service().PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: o.SurfaceID, Kind: control.ActionTextMessage})
			if allowed == held {
				t.Fatalf("allowed=%v held=%v", allowed, held)
			}
		})
	}
}
