package feishu

import "testing"

func TestMatchScopeRequirementTreatsChatAsChatReadonlySatisfier(t *testing.T) {
	scope, ok := MatchScopeRequirement("im:chat:readonly", "tenant", []AppScopeStatus{
		{ScopeName: "im:chat", ScopeType: "tenant", GrantStatus: 1},
	})
	if !ok || scope != "im:chat" {
		t.Fatalf("MatchScopeRequirement = %q, %v; want im:chat satisfier", scope, ok)
	}
}
