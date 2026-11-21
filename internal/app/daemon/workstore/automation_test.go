package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestAutomationPolicyRequiresSpecificHumanChoiceAndSurvivesReopen(t *testing.T) {
	s, path := openTestStore(t)
	gateway := seedMenuRole(t, s, "leader", "app")
	var on, off MenuInteraction
	requireTx(t, s, func(tx *Tx) error {
		raw := menuEnvelope(gateway, "enable", "person")
		raw.EventKey = "autowhip_on"
		var e error
		on, _, e = tx.AcceptMenuInteraction(raw, gateway)
		if e != nil {
			return e
		}
		source := PrivateActionSource{PersonalScopeID: on.ScopeID, MenuInteractionID: on.ID}
		_, proof, e := tx.ValidatePrivateAction(source)
		if e != nil {
			return e
		}
		policy := work.AutomationPolicy{ContextID: on.ContextID, ScopeID: on.ScopeID, Kind: work.AutomationWhip, Enabled: true, Source: proof, Revision: 1, UpdatedAt: time.Now().UTC()}
		if e = tx.PutAutomationPolicy(policy, 0, source); e != nil {
			return e
		}
		forged := policy
		forged.Kind = work.AutomationContinue
		if e = tx.PutAutomationPolicy(forged, 0, source); !errors.Is(e, ErrScopeDenied) {
			t.Fatalf("one setting authorized a different automation: %v", e)
		}
		raw.EventID = "disable"
		raw.EventKey = "autowhip_off"
		off, _, e = tx.AcceptMenuInteraction(raw, gateway)
		return e
	})
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		p, e := tx.GetAutomationPolicy(on.ContextID, work.AutomationWhip)
		if e != nil {
			return e
		}
		if !p.Enabled || p.Revision != 1 {
			t.Fatal("automation policy lost across restart")
		}
		source := PrivateActionSource{PersonalScopeID: off.ScopeID, MenuInteractionID: off.ID}
		_, proof, e := tx.ValidatePrivateAction(source)
		if e != nil {
			return e
		}
		p.Enabled = false
		p.Source = proof
		p.Revision++
		p.UpdatedAt = time.Now().UTC()
		if e = tx.PutAutomationPolicy(p, 1, source); e != nil {
			return e
		}
		p.Revision++
		if e = tx.PutAutomationPolicy(p, 2, source); e != nil {
			return e
		}
		actual, e := tx.GetAutomationPolicy(on.ContextID, work.AutomationWhip)
		if e != nil {
			return e
		}
		if actual.Enabled || actual.Revision != 2 {
			t.Fatal("same human event replay changed policy twice")
		}
		return nil
	})
}
func TestAutomationNativeRetryClassifierAndBoundedSchedule(t *testing.T) {
	for _, code := range []string{"other", "responseStreamDisconnected", "responseTooManyFailedAttempts", "serverOverloaded"} {
		if !retryableAutomationProblem(&agentproto.ErrorInfo{Layer: "gateway", Code: code}) {
			t.Fatalf("lost explicit original retry code %s", code)
		}
	}
	for _, p := range []*agentproto.ErrorInfo{nil, {Layer: "tool", Code: "other"}, {Layer: "gateway", Code: "permissionDenied"}, {Layer: "gateway", Code: "native_goal_budget_exhausted"}} {
		if retryableAutomationProblem(p) {
			t.Fatalf("broadened unsafe retry classifier %+v", p)
		}
	}
	for kind, delays := range map[work.AutomationKind][]time.Duration{work.AutomationWhip: {3 * time.Second, 10 * time.Second, 30 * time.Second}, work.AutomationContinue: {0, 0, 2 * time.Second, 5 * time.Second, 10 * time.Second}} {
		for i, want := range delays {
			got, e := automationDelay(kind, i+1)
			if e != nil || got != want {
				t.Fatalf("wrong bounded delay %s %d %v %v", kind, i, got, e)
			}
		}
		if _, e := automationDelay(kind, len(delays)+1); e == nil {
			t.Fatal("automation continued beyond bounded episode")
		}
	}
}
