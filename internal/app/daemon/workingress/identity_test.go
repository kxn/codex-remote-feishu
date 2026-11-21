package workingress

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestUnionIdentityRequiresVerifiedSharedApplicationRealm(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "unproven realm", true: "verified developer realm"}[shared], func(t *testing.T) {
			ingress, store, directory := ingressFixture(t, false)
			if shared {
				for key, gateway := range directory.gateways {
					gateway.UnionDomain = "verified-developer"
					directory.gateways[key] = gateway
				}
			}
			first := rawMessage("leader", "one", "private-a", "p2p", "app-a-open", "", "one")
			first.SenderIDs.UnionID = "same-union-value"
			second := rawMessage("expert", "two", "private-b", "p2p", "app-b-open", "", "two")
			second.SenderIDs.UnionID = "same-union-value"
			receive(t, ingress, first)
			receive(t, ingress, second)
			process(t, ingress)
			txRun(t, store, func(tx *workstore.Tx) error {
				scopes, err := tx.ListScopes()
				if err != nil {
					return err
				}
				if len(scopes) != 2 {
					t.Fatal("two role scopes not isolated")
				}
				if (scopes[0].PrincipalID == scopes[1].PrincipalID) != shared {
					t.Fatal("union value merged without verified shared application realm")
				}
				return nil
			})
		})
	}
}
func TestIdentityMappingCannotInventAliasesFromAnotherObservedEvent(t *testing.T) {
	ingress, store, _ := ingressFixture(t, true)
	receipt := receive(t, ingress, rawMessage("leader", "real", "group", "group", "actual-open", "actual-user", "real source"))
	txRun(t, store, func(tx *workstore.Tx) error {
		_, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app:leader", Generation: 1, IDs: work.SenderIDs{OpenID: "actual-open", UserID: "victim-user"}, ObservationRef: receipt.IngressID})
		if err == nil {
			t.Fatal("unobserved victim identity was accepted as SDK proof")
		}
		return nil
	})
}
