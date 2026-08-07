package gateway

import (
	"testing"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestActionPayloadKindReadsCanonicalKindKey(t *testing.T) {
	value := map[string]any{
		frontstagecontract.CardActionPayloadKeyKind: "  " + cardActionKindShowAllThreads + "  ",
	}
	if got := actionPayloadKind(value); got != cardActionKindShowAllThreads {
		t.Fatalf("actionPayloadKind() = %q, want %q", got, cardActionKindShowAllThreads)
	}
}
