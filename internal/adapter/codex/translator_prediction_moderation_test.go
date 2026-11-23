package codex

import (
	"encoding/json"
	"fmt"
	"testing"
)

func observePredictionMetadata(t *testing.T, tr *Translator, thread, turn, metadata string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"method": "turn/moderationMetadata", "params": map[string]any{"threadId": thread, "turnId": turn, "metadata": json.RawMessage(metadata)}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tr.ObserveServer(raw)
	if err != nil || result.Suppress || len(result.Events) != 0 {
		t.Fatalf("native metadata forwarding changed: %#v %v", result, err)
	}
}

func moderatedPredictionSuggestion(t *testing.T, tr *Translator, thread, turn string) string {
	t.Helper()
	command := predictionCommand("prediction:" + thread + ":" + turn)
	command.Target.ThreadID = thread
	command.Prediction.SourceTurnID = turn
	request := predictionRequest(t, tr, command)
	result := predictionResponse(t, tr, request["id"], map[string]any{"sourceTurnId": turn, "text": `{"suggestion":"Unclassified suggestion text"}`}, nil)
	if !result.Suppress || len(result.Events) != 1 || result.Events[0].Prediction == nil || result.Events[0].ErrorMessage != "" {
		t.Fatalf("moderation changed owned result contract: %#v", result)
	}
	return result.Events[0].Prediction.Suggestion
}

func TestPredictionModerationMatchesDesktopStagesAndThreshold(t *testing.T) {
	policy := json.RawMessage(`{"st":{"score":0.7},"ubl":["prompt-block"],"abl":["generation-block"]}`)
	for _, tc := range []struct {
		name, metadata string
		blocked        bool
	}{
		{"prompt score equals threshold", `{"prompt":{"provider":{"outputs":[{"results":[{"_modapi_scores":{"score":0.7}}]}]}}}`, true},
		{"prompt score greater", `{"prompt":{"provider":{"outputs":[{"results":[{"_modapi_scores":{"score":0.8}}]}]}}}`, true},
		{"prompt below threshold", `{"prompt":{"provider":{"outputs":[{"results":[{"_modapi_scores":{"score":0.69}}]}]}}}`, false},
		{"prompt label", `{"prompt":{"provider":{"outputs":[{"results":[{"labels":["prompt-block"]}]}]}}}`, true},
		{"generation label", `{"generation":{"provider":{"outputs":[{"results":[{"labels":["generation-block"]}]}]}}}`, true},
		{"generation scores ignored", `{"generation":{"provider":{"outputs":[{"results":[{"_modapi_scores":{"score":1}}]}]}}}`, false},
		{"generation ignores prompt labels", `{"generation":{"provider":{"outputs":[{"results":[{"labels":["prompt-block"]}]}]}}}`, false},
		{"prompt ignores generation labels", `{"prompt":{"provider":{"outputs":[{"results":[{"labels":["generation-block"]}]}]}}}`, false},
		{"later provider output result", `{"prompt":{"first":{"outputs":[]},"second":{"outputs":[{"results":[]},{"results":[{}, {"labels":["prompt-block"]}]}]}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("host")
			tr.SetPredictionPolicy(policy)
			observePredictionMetadata(t, tr, "thread", "turn", tc.metadata)
			if got := moderatedPredictionSuggestion(t, tr, "thread", "turn"); (got == "") != tc.blocked {
				t.Fatalf("suggestion=%q blocked=%v", got, tc.blocked)
			}
		})
	}
}

func TestPredictionModerationInvalidPolicyDoesNotInventFiltering(t *testing.T) {
	for _, raw := range []string{"", `null`, `[]`, `{"st":null}`, `{"st":{"score":"0.1"}}`, `{"ubl":null}`, `{"ubl":[null]}`, `{"abl":[1]}`, `{}`} {
		tr := NewTranslator("host")
		tr.SetPredictionPolicy(json.RawMessage(`{"ubl":["blocked"]}`))
		tr.SetPredictionPolicy(json.RawMessage(raw))
		observePredictionMetadata(t, tr, "thread", "turn", `{"prompt":{"provider":{"outputs":[{"results":[{"labels":["blocked"]}]}]}}}`)
		if got := moderatedPredictionSuggestion(t, tr, "thread", "turn"); got == "" {
			t.Fatalf("missing/invalid/empty policy filtered text: %s", raw)
		}
	}
}

func TestPredictionModerationInvalidMetadataSkipsWholeEntry(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"prompt":null}`, `{"prompt":{"provider":{}}}`, `{"prompt":{"provider":{"outputs":null}}}`, `{"prompt":{"provider":{"outputs":[{}]}}}`, `{"prompt":{"provider":{"outputs":[{"results":[{"labels":[null]}]}]}}}`, `{"prompt":{"provider":{"outputs":[{"results":[{"_modapi_scores":null}]}]}}}`, `{"prompt":{"provider":{"outputs":[{"results":[{"labels":["blocked"]}]}]}},"generation":null}`} {
		tr := NewTranslator("host")
		tr.SetPredictionPolicy(json.RawMessage(`{"ubl":["blocked"]}`))
		observePredictionMetadata(t, tr, "thread", "turn", raw)
		if got := moderatedPredictionSuggestion(t, tr, "thread", "turn"); got == "" {
			t.Fatalf("invalid metadata entry filtered text: %s", raw)
		}
	}
}

func TestPredictionModerationUsesCurrentPolicyAndAccumulatedSourceMetadata(t *testing.T) {
	tr := NewTranslator("host")
	request := predictionRequest(t, tr, predictionCommand("pending"))
	observePredictionMetadata(t, tr, "source-thread", "source-turn", `null`)
	observePredictionMetadata(t, tr, "other-thread", "source-turn", `{"prompt":{"p":{"outputs":[{"results":[{"labels":["unrelated"]}]}]}}}`)
	observePredictionMetadata(t, tr, "source-thread", "source-turn", `{"prompt":{"p":{"outputs":[{"results":[{"labels":["blocked"]}]}]}}}`)
	observePredictionMetadata(t, tr, "source-thread", "source-turn", `{}`)
	// Desktop reads the policy when generation completes, not when it starts.
	tr.SetPredictionPolicy(json.RawMessage(`{"ubl":["blocked"]}`))
	result := predictionResponse(t, tr, request["id"], map[string]any{"sourceTurnId": "source-turn", "text": `{"suggestion":"Keep the raw text unchanged"}`}, nil)
	if len(result.Events) != 1 || result.Events[0].Prediction.Suggestion != "" {
		t.Fatalf("current policy did not filter accumulated source metadata: %#v", result)
	}
	tr.SetPredictionPolicy(nil)
	if got := moderatedPredictionSuggestion(t, tr, "source-thread", "source-turn"); got == "" {
		t.Fatal("cleared policy remained active")
	}
	tr.SetPredictionPolicy(json.RawMessage(`{"ubl":["blocked"]}`))
	if got := moderatedPredictionSuggestion(t, tr, "source-thread", "other-turn"); got == "" {
		t.Fatal("metadata crossed source turn boundary")
	}
	if got := moderatedPredictionSuggestion(t, tr, "other-thread", "source-turn"); got == "" {
		t.Fatal("metadata crossed thread boundary")
	}
}

func TestPredictionModerationKeepsDesktop64SourceFIFO(t *testing.T) {
	tr := NewTranslator("host")
	tr.SetPredictionPolicy(json.RawMessage(`{"ubl":["blocked"]}`))
	blocked := `{"prompt":{"p":{"outputs":[{"results":[{"labels":["blocked"]}]}]}}}`
	for i := 0; i < 64; i++ {
		observePredictionMetadata(t, tr, "thread", fmt.Sprint(i), blocked)
	}
	// Appending metadata for an existing key does not make it newest in desktop Map.
	observePredictionMetadata(t, tr, "thread", "0", `{}`)
	observePredictionMetadata(t, tr, "thread", "64", blocked)
	if got := moderatedPredictionSuggestion(t, tr, "thread", "0"); got == "" {
		t.Fatal("oldest source was not evicted after 65 keys")
	}
	if got := moderatedPredictionSuggestion(t, tr, "thread", "1"); got != "" {
		t.Fatal("non-oldest source was evicted")
	}
	if got := moderatedPredictionSuggestion(t, tr, "thread", "64"); got != "" {
		t.Fatal("newest source metadata missing")
	}
}
