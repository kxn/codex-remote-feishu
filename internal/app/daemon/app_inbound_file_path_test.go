package daemon

import "testing"

func TestInboundFileSameCleanPathUsesPlatformCanonicalComparison(t *testing.T) {
	if !sameCleanPath(`\\?\C:\Users\Codex\Downloads\spec.pdf`, `c:/users/codex/downloads/spec.pdf`) {
		t.Fatal("expected Windows-like inbound file paths to compare by canonical identity")
	}
	if sameCleanPath("", `c:/users/codex/downloads/spec.pdf`) {
		t.Fatal("did not expect empty inbound file path to match a real path")
	}
}
