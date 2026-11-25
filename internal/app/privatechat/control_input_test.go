package privatechat

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestControlInputRequiresExactOwnerKindAndAppliedIngress(t *testing.T) {
	j, _, _, owner, _ := effectFixture(t)
	a := privateAction(owner, control.ActionReviewApply)
	a.MessageID = "platform-card"
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	seq, _, err := j.Admit(t.Context(), Ingress{Owner: owner, Generation: 1, EventID: "click", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifyControlInput(t.Context(), j.db, owner, 1, "click", seq, false); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyControlInput(t.Context(), j.db, owner, 1, "click", 0, true); err == nil {
		t.Fatal("unapplied click authorized dispatch")
	}
	if _, err = j.db.ExecContext(t.Context(), `INSERT INTO private_applied(seq,revision) VALUES(?,?)`, seq, 99); err != nil {
		t.Fatal(err)
	}
	if got, e := verifyControlInput(t.Context(), j.db, owner, 1, "click", 0, true); e != nil || got != seq {
		t.Fatalf("applied click: seq=%d err=%v", got, e)
	}
	wrong := privateAction(owner, control.ActionStatus)
	wrongRaw, err := json.Marshal(wrong)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = j.Admit(t.Context(), Ingress{Owner: owner, Generation: 1, EventID: "wrong-kind", Lane: "control", Payload: wrongRaw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifyControlInput(t.Context(), j.db, owner, 1, "wrong-kind", 0, false); err == nil {
		t.Fatal("unrelated control authorized input")
	}
	foreign := owner
	foreign.UserID = "another-user"
	if _, err = verifyControlInput(t.Context(), j.db, foreign, 1, "click", 0, true); err == nil {
		t.Fatal("foreign owner authorized")
	}
	if _, err = verifyControlInput(t.Context(), j.db, owner, 2, "click", 0, true); err == nil {
		t.Fatal("foreign epoch authorized")
	}
	if _, err = verifyControlInput(t.Context(), j.db, owner, 1, "platform-card", 0, true); err == nil {
		t.Fatal("platform card treated as ingress")
	}
}

func TestControlInputPlanDecisionRequiresExecutableOption(t *testing.T) {
	j, _, _, owner, _ := effectFixture(t)
	for _, tc := range []struct {
		option  string
		allowed bool
	}{{"execute", true}, {"execute_new", true}, {"cancel", false}, {"invalid", false}, {"", false}} {
		t.Run(tc.option, func(t *testing.T) {
			a := privateAction(owner, control.ActionPlanProposalDecision)
			a.OptionID = tc.option
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			id := "plan-" + tc.option
			seq, _, err := j.Admit(t.Context(), Ingress{Owner: owner, Generation: 1, EventID: id, Lane: "control", Payload: raw})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.ExecContext(t.Context(), `INSERT INTO private_applied(seq,revision) VALUES(?,?)`, seq, seq+100); err != nil {
				t.Fatal(err)
			}
			for _, applied := range []bool{false, true} {
				_, err = verifyControlInput(t.Context(), j.db, owner, 1, id, seq, applied)
				if (err == nil) != tc.allowed {
					t.Fatalf("option=%q applied=%v: %v", tc.option, applied, err)
				}
			}
		})
	}
}
