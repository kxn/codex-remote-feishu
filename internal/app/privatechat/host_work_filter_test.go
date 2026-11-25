package privatechat

import (
	"fmt"
	"path/filepath"
	"testing"
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
