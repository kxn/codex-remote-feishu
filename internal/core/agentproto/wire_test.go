package agentproto

import (
	"strings"
	"testing"
)

func TestEnvelopeExplorationCarrierRoundTrip(t *testing.T) {
	tests := []struct {
		name              string
		exploration       *ExplorationActions
		wantJSON          string
		wantExploration   bool
		wantActionKinds   []ExplorationActionKind
		wantFirstReadItem string
	}{
		{
			name:     "producer did not classify",
			wantJSON: `"kind":"item.started"`,
		},
		{
			name:            "producer authoritatively rejected exploration",
			exploration:     &ExplorationActions{},
			wantJSON:        `"exploration":{}`,
			wantExploration: true,
		},
		{
			name: "producer supplied ordered actions",
			exploration: &ExplorationActions{Actions: []ExplorationAction{
				{Kind: ExplorationActionRead, Items: []string{"a.go"}},
				{Kind: ExplorationActionSearch, Summary: "needle", Secondary: "internal/"},
			}},
			wantJSON:          `"exploration":{"actions":[`,
			wantExploration:   true,
			wantActionKinds:   []ExplorationActionKind{ExplorationActionRead, ExplorationActionSearch},
			wantFirstReadItem: "a.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := MarshalEnvelope(Envelope{
				Type: EnvelopeEventBatch,
				EventBatch: &EventBatch{
					InstanceID: "inst-1",
					Events: []Event{{
						Kind:        EventItemStarted,
						Exploration: tt.exploration,
					}},
				},
			})
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			if !strings.Contains(string(raw), tt.wantJSON) {
				t.Fatalf("expected JSON to contain %q, got %s", tt.wantJSON, raw)
			}
			if tt.exploration == nil && strings.Contains(string(raw), `"exploration"`) {
				t.Fatalf("expected nil exploration carrier to be omitted, got %s", raw)
			}

			decoded, err := UnmarshalEnvelope(raw)
			if err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if decoded.EventBatch == nil || len(decoded.EventBatch.Events) != 1 {
				t.Fatalf("unexpected decoded envelope: %#v", decoded)
			}
			got := decoded.EventBatch.Events[0].Exploration
			if (got != nil) != tt.wantExploration {
				t.Fatalf("unexpected exploration presence: %#v", got)
			}
			if len(tt.wantActionKinds) == 0 {
				if got != nil && len(got.Actions) != 0 {
					t.Fatalf("expected no actions, got %#v", got.Actions)
				}
				return
			}
			if got == nil || len(got.Actions) != len(tt.wantActionKinds) {
				t.Fatalf("unexpected decoded actions: %#v", got)
			}
			for i, wantKind := range tt.wantActionKinds {
				if got.Actions[i].Kind != wantKind {
					t.Fatalf("action %d kind = %q, want %q", i, got.Actions[i].Kind, wantKind)
				}
			}
			if len(got.Actions[0].Items) != 1 || got.Actions[0].Items[0] != tt.wantFirstReadItem {
				t.Fatalf("unexpected read items: %#v", got.Actions[0].Items)
			}
		})
	}
}
