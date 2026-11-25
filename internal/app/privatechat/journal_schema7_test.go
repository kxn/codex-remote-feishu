package privatechat

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDispatchSchema7MigrationKeepsRowsReferencesAndIndexes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.db.Exec(`INSERT INTO ingress(gateway,tenant,app_id,surface,chat,user_id,generation,event_id,lane,payload,sources) VALUES('g','t','a','s','c','u',1,'e','input','{}','[]');
 INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('d','g','t','a','s','c','u',1,'input','start','codex','ns','','inc',1,'cmd','prepared');
 INSERT INTO dispatch_input(dispatch_id,seq) VALUES('d',1);
 INSERT INTO native_commands(dispatch_id,command_id,payload,payload_hash) VALUES('d','cmd','{}','hash');
 CREATE INDEX dispatch_test_owner ON dispatch(surface); PRAGMA user_version=6;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var version, n int
	if err = j.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != privateJournalSchemaVersion {
		t.Fatalf("version=%d %v", version, err)
	}
	if err = j.db.QueryRow(`SELECT count(*) FROM dispatch d JOIN dispatch_input i ON i.dispatch_id=d.id JOIN native_commands n ON n.dispatch_id=d.id WHERE d.id='d' AND d.command_id='cmd'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("references=%d %v", n, err)
	}
	if err = j.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='dispatch_test_owner'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("index lost")
	}
	if _, err = j.db.Exec(`UPDATE dispatch SET state='sent' WHERE id='d'`); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchSchema7MigrationRollsBackOnForeignKeyFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`PRAGMA foreign_keys=OFF; INSERT INTO dispatch_input(dispatch_id,seq) VALUES('missing',1); PRAGMA user_version=6;`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	if next, e := OpenJournal(path); e == nil {
		next.Close()
		t.Fatal("invalid foreign keys accepted")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, n int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 6 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM dispatch_input WHERE dispatch_id='missing'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("original row lost: %d %v", n, err)
	}
}
