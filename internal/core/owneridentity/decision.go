package owneridentity

import "strings"

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionDeny            Decision = "deny"
	DecisionIdentityMissing Decision = "identity_missing"
)

func Decide(ownerUserID string, actorUserIDs ...string) Decision {
	owner := strings.TrimSpace(ownerUserID)
	if owner == "" {
		return DecisionAllow
	}
	actor := ""
	for _, candidate := range actorUserIDs {
		if value := strings.TrimSpace(candidate); value != "" {
			actor = value
			break
		}
	}
	if actor == "" {
		return DecisionIdentityMissing
	}
	if actor != owner {
		return DecisionDeny
	}
	return DecisionAllow
}
