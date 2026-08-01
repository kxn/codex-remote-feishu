package owneridentity

import "testing"

func TestDecideOwnerIdentity(t *testing.T) {
	tests := []struct {
		name   string
		owner  string
		actors []string
		want   Decision
	}{
		{name: "ownerless flow allows", owner: "", actors: nil, want: DecisionAllow},
		{name: "matching action actor allows", owner: "user-1", actors: []string{"user-1"}, want: DecisionAllow},
		{name: "matching fallback actor allows", owner: "user-1", actors: []string{"", "user-1"}, want: DecisionAllow},
		{name: "mismatched actor denies", owner: "user-1", actors: []string{"user-2"}, want: DecisionDeny},
		{name: "missing actor fails closed", owner: "user-1", actors: []string{"", " "}, want: DecisionIdentityMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.owner, tt.actors...); got != tt.want {
				t.Fatalf("Decide(%q, %q) = %q, want %q", tt.owner, tt.actors, got, tt.want)
			}
		})
	}
}
