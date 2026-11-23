package wrapper

import "testing"

func TestPrivateOpenCodeVersionFromActualHandshake(t *testing.T) {
	for _, tc := range []struct {
		v  string
		ok bool
	}{{"2.1.4", true}, {"2.2.0", true}, {"2.1.3", false}, {"1.18.7", false}, {"3.0.0", false}, {"2.1.4-rc1", false}, {"", false}} {
		raw := []byte(`{"result":{"agentInfo":{"name":"OpenCode","version":"` + tc.v + `"}}}`)
		if got := verifyPrivateOpenCodeBootstrap(raw) == nil; got != tc.ok {
			t.Fatalf("%s: %v", tc.v, got)
		}
	}
	if verifyPrivateOpenCodeBootstrap([]byte(`{"result":{"agentInfo":{"name":"other","version":"2.1.4"}}}`)) == nil {
		t.Fatal("different agent accepted")
	}
}
