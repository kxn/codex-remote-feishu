package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestNewDecisionCannotReadoptPreviousAgreementOwner(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	source := instruction(t, s, "human-change")
	requireTx(t, s, func(tx *Tx) error {
		current := agreement(source)
		if e := tx.PutAgreement(current, 0); e != nil {
			return e
		}
		current.Revision++
		current.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "r"}
		if e := tx.PutAgreement(current, 1); e != nil {
			return e
		}
		request := decisionRequest()
		if e := tx.PutDecisionRequest(request, 0); e == nil {
			t.Fatal("new decision restored old human owner using historical revision")
		}
		request.AgreementRevision = current.Revision
		request.Owner = current.DecisionOwner
		return tx.PutDecisionRequest(request, 0)
	})
}
