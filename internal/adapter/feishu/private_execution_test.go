package feishu

import (
	"strings"
	"testing"
)

func TestPrivateExecutionLongMarkdownKeepsAllChunks(t *testing.T) {
	body := "first anchor\n\n" + strings.Repeat("测试abcdefghijklmnopqrstuvwxyz", 2500) + "\n\nlast anchor"
	cards := PrivateExecutionReply("", body, nil, nil)
	if len(cards) < 2 {
		t.Fatalf("long final did not split: %d", len(cards))
	}
	for i, card := range cards {
		if len(card) == 0 || len(card) > 28*1024 {
			t.Fatalf("private final chunk %d is empty or over the send limit: %d", i+1, len(card))
		}
	}
	if !strings.Contains(string(cards[0]), "first anchor") || !strings.Contains(string(cards[len(cards)-1]), "last anchor") {
		t.Fatal("private final chunk split lost the beginning or tail")
	}
}
