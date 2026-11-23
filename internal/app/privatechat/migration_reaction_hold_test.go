package privatechat

import (
	"path/filepath"
	"testing"
)

func TestMigratedReactionHoldIsOwnerAndMessageScoped(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{TenantID: "tenant", AppID: "app", GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user"}
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('i','s','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"reaction_payload":{"message":{"tenant":"tenant","chat_id":"chat","message_id":"old-message"}}}`)
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_records(import_id,source_table,source_key,source_scope,source_order,source_revision,payload,payload_hash,data,data_hash) VALUES('i','personal_migration_reaction_holds','r','s',1,1,?,?,?,?)`, raw, migrationSHA(raw), []byte{}, migrationSHA(nil))
	if err != nil {
		t.Fatal(err)
	}
	held, err := j.HasUncertainMigratedReaction(ctx, o, 1, "old-message")
	if err != nil || !held {
		t.Fatalf("old chain: %v %v", held, err)
	}
	held, err = j.HasUncertainMigratedReaction(ctx, o, 1, "new-message")
	if err != nil || held {
		t.Fatalf("new message held: %v %v", held, err)
	}
	foreign := o
	foreign.UserID = "other"
	held, err = j.HasUncertainMigratedReaction(ctx, foreign, 1, "old-message")
	if err != nil || held {
		t.Fatalf("foreign owner held: %v %v", held, err)
	}
	_, err = j.db.ExecContext(ctx, `UPDATE private_migration_records SET payload='{}' WHERE import_id='i'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.HasUncertainMigratedReaction(ctx, o, 1, "new-message"); err == nil {
		t.Fatal("corrupt source hold ignored")
	}
}
