package privatechat

import (
	"path/filepath"
	"testing"
)

func TestPhysicalExitKeepsUncertainBusinessSeparate(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	d := Dispatch{ID: "command", Owner: o, Generation: 1, CommandID: "command", Lane: "control", Mode: "none", Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}}
	tx, err := j.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareDispatchWithCauseTx(t.Context(), tx, d, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		state string
		want  bool
	}{{"prepared", true}, {"sending", true}, {"unknown", true}, {"sent", true}, {"accepted", false}, {"resolved", false}, {"failed", false}} {
		if _, err = j.db.Exec(`UPDATE dispatch SET state=? WHERE id='command'`, v.state); err != nil {
			t.Fatal(err)
		}
		got, err := j.CarrierHasUnsettledBusiness(t.Context(), o, 1, "inc")
		if err != nil || got != v.want {
			t.Fatalf("state=%s got=%v err=%v", v.state, got, err)
		}
		foreign := o
		foreign.UserID = "other"
		got, err = j.CarrierHasUnsettledBusiness(t.Context(), foreign, 1, "inc")
		if err != nil || got {
			t.Fatalf("foreign source affected: %v %v", got, err)
		}
	}
}
