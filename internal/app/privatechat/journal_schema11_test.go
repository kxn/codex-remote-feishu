package privatechat

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSchema11BackfillsOnlyLegacyReadObservationDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.db.Exec(`INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('old:native','g','t','a','s','c','u',1,'inspect','resume','codex','ns','thread','inc',1,'cmd','unknown')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.db.Exec(`INSERT INTO native_commands(dispatch_id,command_id,payload,payload_hash) VALUES('old:native','cmd','{"kind":"thread.read"}','original-hash')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`DROP TABLE private_read_observations`); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`PRAGMA user_version=10`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	var state, hash string
	var expires int64
	if err = j.db.QueryRow(`SELECT d.state,n.payload_hash,r.expires_ns FROM dispatch d JOIN native_commands n ON n.dispatch_id=d.id JOIN private_read_observations r ON r.dispatch_id=d.id WHERE d.id='old:native'`).Scan(&state, &hash, &expires); err != nil {
		t.Fatal(err)
	}
	if state != "unknown" || hash != "original-hash" || expires < before.Add(119*time.Second).UnixNano() || expires > time.Now().Add(121*time.Second).UnixNano() {
		t.Fatalf("migration changed legacy evidence or deadline: state=%s hash=%s expiry=%v", state, hash, time.Unix(0, expires))
	}
}
