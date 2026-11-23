package codex

import (
	"encoding/json"
	"slices"
)

type predictionModerationPolicy struct {
	thresholds                     map[string]float64
	promptLabels, generationLabels []string
}

type predictionModerationSource struct{ threadID, turnID string }

type predictionModerationState struct {
	policy   *predictionModerationPolicy
	metadata map[predictionModerationSource][]any
	order    []predictionModerationSource
}

type predictionModerationResult struct {
	labels []string
	scores map[string]float64
}

// SetPredictionPolicy receives the current official desktop dynamic configuration.
// Like other Translator methods, callers serialize it with server observation.
func (t *Translator) SetPredictionPolicy(raw json.RawMessage) {
	t.predictionModeration.policy = nil
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return
	}
	policy := &predictionModerationPolicy{}
	var valid bool
	if value, exists := object["st"]; exists {
		policy.thresholds, valid = predictionModerationScores(value)
		if !valid {
			return
		}
	}
	if value, exists := object["ubl"]; exists {
		policy.promptLabels, valid = predictionModerationLabels(value)
		if !valid {
			return
		}
	}
	if value, exists := object["abl"]; exists {
		policy.generationLabels, valid = predictionModerationLabels(value)
		if !valid {
			return
		}
	}
	t.predictionModeration.policy = policy
}

func predictionModerationLabels(value any) ([]string, bool) {
	values, valid := value.([]any)
	if !valid {
		return nil, false
	}
	labels := make([]string, 0, len(values))
	for _, value := range values {
		label, valid := value.(string)
		if !valid {
			return nil, false
		}
		labels = append(labels, label)
	}
	return labels, true
}

func predictionModerationScores(value any) (map[string]float64, bool) {
	values, valid := value.(map[string]any)
	if !valid {
		return nil, false
	}
	scores := make(map[string]float64, len(values))
	for key, value := range values {
		score, valid := value.(float64)
		if !valid {
			return nil, false
		}
		scores[key] = score
	}
	return scores, true
}

func (t *Translator) observePredictionModeration(message map[string]any) {
	params, valid := message["params"].(map[string]any)
	if !valid {
		return
	}
	threadID, threadValid := params["threadId"].(string)
	turnID, turnValid := params["turnId"].(string)
	metadata, exists := params["metadata"]
	if !threadValid || !turnValid || !exists {
		return
	}
	state := &t.predictionModeration
	if state.metadata == nil {
		state.metadata = make(map[predictionModerationSource][]any)
	}
	source := predictionModerationSource{threadID, turnID}
	if _, exists := state.metadata[source]; !exists {
		state.order = append(state.order, source)
	}
	state.metadata[source] = append(state.metadata[source], metadata)
	// Desktop Map evicts the first inserted source; appending to a source is not LRU.
	if len(state.metadata) > 64 {
		delete(state.metadata, state.order[0])
		state.order = state.order[1:]
	}
}

func parsePredictionModerationSide(value any) ([]predictionModerationResult, bool) {
	providers, valid := value.(map[string]any)
	if !valid {
		return nil, false
	}
	var parsed []predictionModerationResult
	for _, value := range providers {
		provider, valid := value.(map[string]any)
		if !valid {
			return nil, false
		}
		outputs, valid := provider["outputs"].([]any)
		if !valid {
			return nil, false
		}
		for _, value := range outputs {
			output, valid := value.(map[string]any)
			if !valid {
				return nil, false
			}
			results, valid := output["results"].([]any)
			if !valid {
				return nil, false
			}
			for _, value := range results {
				result, valid := value.(map[string]any)
				if !valid {
					return nil, false
				}
				item := predictionModerationResult{}
				if labels, exists := result["labels"]; exists {
					item.labels, valid = predictionModerationLabels(labels)
					if !valid {
						return nil, false
					}
				}
				if scores, exists := result["_modapi_scores"]; exists {
					item.scores, valid = predictionModerationScores(scores)
					if !valid {
						return nil, false
					}
				}
				parsed = append(parsed, item)
			}
		}
	}
	return parsed, true
}

func (t *Translator) suppressPredictionByModeration(threadID, turnID string) bool {
	policy := t.predictionModeration.policy
	if policy == nil {
		return false
	}
	for _, value := range t.predictionModeration.metadata[predictionModerationSource{threadID, turnID}] {
		metadata, valid := value.(map[string]any)
		if !valid {
			continue
		}
		var prompt, generation []predictionModerationResult
		if value, exists := metadata["prompt"]; exists {
			prompt, valid = parsePredictionModerationSide(value)
			if !valid {
				continue
			}
		}
		if value, exists := metadata["generation"]; exists {
			generation, valid = parsePredictionModerationSide(value)
			if !valid {
				continue
			}
		}
		// V8 validates the entire metadata entry before considering either stage.
		for _, result := range prompt {
			for _, label := range result.labels {
				if slices.Contains(policy.promptLabels, label) {
					return true
				}
			}
			for key, threshold := range policy.thresholds {
				if score, exists := result.scores[key]; exists && score >= threshold {
					return true
				}
			}
		}
		for _, result := range generation {
			for _, label := range result.labels {
				if slices.Contains(policy.generationLabels, label) {
					return true
				}
			}
		}
	}
	return false
}
