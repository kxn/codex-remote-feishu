package agentproto

import (
	"encoding/json"
	"testing"
)

func TestPredictionWirePayloadRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		out  any
	}{
		{"request", `{"kind":"thread.prediction.request","commandId":"prediction-1","prediction":{"sourceTurnId":"turn-1","instanceId":"instance-original"}}`, &Command{}},
		{"cancel", `{"kind":"thread.prediction.cancel","prediction":{"requestCommandId":"prediction-1"}}`, &Command{}},
		{"result", `{"kind":"thread.prediction.result","prediction":{"sourceTurnId":"turn-1","suggestion":"继续检查测试","unsupported":true}}`, &Event{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.raw), tc.out); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(tc.out)
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]json.RawMessage
			_ = json.Unmarshal([]byte(tc.raw), &want)
			_ = json.Unmarshal(encoded, &got)
			if string(got["prediction"]) != string(want["prediction"]) {
				t.Fatalf("prediction payload lost in round trip: %s", encoded)
			}
		})
	}
}

func TestPredictionCapabilityRequiresExplicitDeclaration(t *testing.T) {
	var declared Capabilities
	if err := json.Unmarshal([]byte(`{"threadPrediction":true}`), &declared); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]bool
	_ = json.Unmarshal(encoded, &fields)
	if !fields["threadPrediction"] {
		t.Fatalf("explicit prediction capability lost: %s", encoded)
	}
	if !EffectiveCapabilitiesForBackend(BackendCodex, declared).ThreadPrediction {
		t.Fatal("effective capabilities must preserve an explicit prediction declaration")
	}
	for _, backend := range []Backend{BackendCodex, BackendClaude, BackendOpenCode} {
		encoded, _ := json.Marshal(DefaultCapabilitiesForBackend(backend))
		var legacy map[string]bool
		_ = json.Unmarshal(encoded, &legacy)
		if legacy["threadPrediction"] {
			t.Fatalf("legacy %s runtime must not gain prediction implicitly", backend)
		}
	}
}
