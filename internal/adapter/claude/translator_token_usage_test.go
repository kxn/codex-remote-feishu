package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestBuildClaudeTokenUsageMapsTotalInputAndCacheReadSeparately(t *testing.T) {
	previousWindow := 100000
	usage := buildClaudeTokenUsage(map[string]any{
		"usage": map[string]any{
			"input_tokens":                70,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens":     50,
			"output_tokens":               30,
		},
		"modelUsage": map[string]any{
			"mimo-v2.5-pro": map[string]any{"contextWindow": 200000},
		},
	}, &agentproto.ThreadTokenUsage{
		Total: agentproto.TokenUsageBreakdown{
			InputTokens:       10,
			CachedInputTokens: 4,
			OutputTokens:      5,
			TotalTokens:       15,
		},
		ModelContextWindow: &previousWindow,
	})
	if usage == nil {
		t.Fatal("expected token usage")
	}
	if usage.Last.InputTokens != 140 || usage.Last.CachedInputTokens != 50 || usage.Last.OutputTokens != 30 || usage.Last.TotalTokens != 170 {
		t.Fatalf("unexpected last usage mapping: %#v", usage.Last)
	}
	if usage.Total.InputTokens != 150 || usage.Total.CachedInputTokens != 54 || usage.Total.OutputTokens != 35 || usage.Total.TotalTokens != 185 {
		t.Fatalf("unexpected cumulative usage mapping: %#v", usage.Total)
	}
	if usage.ModelContextWindow == nil || *usage.ModelContextWindow != 200000 {
		t.Fatalf("unexpected model context window: %#v", usage.ModelContextWindow)
	}
}

func TestClaudeTranslatorAccumulatesThreadTokenUsageAcrossTurns(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	first := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"is_error":   false,
		"result":     "done 1",
		"session_id": threadID,
		"usage": map[string]any{
			"input_tokens":                80,
			"cache_creation_input_tokens": 10,
			"cache_read_input_tokens":     40,
			"output_tokens":               20,
		},
		"modelUsage": map[string]any{
			"mimo-v2.5-pro": map[string]any{"contextWindow": 200000},
		},
	})
	var firstUsage *agentproto.ThreadTokenUsage
	for _, event := range first.Events {
		if event.Kind == agentproto.EventThreadTokenUsageUpdated {
			firstUsage = event.TokenUsage
			break
		}
	}
	if firstUsage == nil {
		t.Fatalf("expected usage event in first turn, got %#v", first.Events)
	}
	if firstUsage.Last.InputTokens != 130 || firstUsage.Last.CachedInputTokens != 40 || firstUsage.Total.InputTokens != 130 || firstUsage.Total.TotalTokens != 150 {
		t.Fatalf("unexpected first-turn token usage: %#v", firstUsage)
	}
	last := first.Events[len(first.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.ThreadID != threadID || last.TurnID != turnID {
		t.Fatalf("unexpected first completion event: %#v", last)
	}

	threadID2, turnID2 := startClaudeTurn(t, tr, "default")
	if threadID2 != threadID {
		t.Fatalf("expected Claude session thread to stay stable across turns, got %q vs %q", threadID2, threadID)
	}
	second := observeClaude(t, tr, map[string]any{
		"type":       "result",
		"subtype":    "success",
		"is_error":   false,
		"result":     "done 2",
		"session_id": threadID2,
		"usage": map[string]any{
			"input_tokens":                20,
			"cache_creation_input_tokens": 30,
			"cache_read_input_tokens":     60,
			"output_tokens":               15,
		},
	})
	var secondUsage *agentproto.ThreadTokenUsage
	for _, event := range second.Events {
		if event.Kind == agentproto.EventThreadTokenUsageUpdated {
			secondUsage = event.TokenUsage
			break
		}
	}
	if secondUsage == nil {
		t.Fatalf("expected usage event in second turn, got %#v", second.Events)
	}
	if secondUsage.Last.InputTokens != 110 || secondUsage.Last.CachedInputTokens != 60 || secondUsage.Last.OutputTokens != 15 || secondUsage.Last.TotalTokens != 125 {
		t.Fatalf("unexpected second-turn last usage: %#v", secondUsage.Last)
	}
	if secondUsage.Total.InputTokens != 240 || secondUsage.Total.CachedInputTokens != 100 || secondUsage.Total.OutputTokens != 35 || secondUsage.Total.TotalTokens != 275 {
		t.Fatalf("unexpected second-turn cumulative usage: %#v", secondUsage.Total)
	}
	if secondUsage.ModelContextWindow == nil || *secondUsage.ModelContextWindow != 200000 {
		t.Fatalf("expected model context window to persist, got %#v", secondUsage.ModelContextWindow)
	}
	last = second.Events[len(second.Events)-1]
	if last.Kind != agentproto.EventTurnCompleted || last.ThreadID != threadID2 || last.TurnID != turnID2 {
		t.Fatalf("unexpected second completion event: %#v", last)
	}
}
