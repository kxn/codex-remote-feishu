package state

import "testing"

func TestProfileDefinitionAndPreferenceETagsAreStrongAndIndependent(t *testing.T) {
	definition := CodexProfileDefinitionETag("team-proxy", 3)
	preference := CodexContextPreferenceETag("team-proxy", 3)
	if definition == preference {
		t.Fatalf("definition and preference ETags must differ: %q", definition)
	}
	for name, value := range map[string]string{"definition": definition, "preference": preference} {
		if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
			t.Fatalf("%s ETag %q is not a strong quoted validator", name, value)
		}
		if len(value) >= 2 && value[:2] == "W/" {
			t.Fatalf("%s ETag %q must not be weak", name, value)
		}
	}
}
