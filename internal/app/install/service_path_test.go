package install

import "testing"

func TestInstallSamePathUsesPlatformCanonicalComparison(t *testing.T) {
	if !samePath(`\\?\C:\Users\Codex\bin\relay.exe`, `c:/users/codex/bin/relay.exe`) {
		t.Fatal("expected Windows-like install paths to compare by canonical identity")
	}
	if samePath("", `c:/users/codex/bin/relay.exe`) {
		t.Fatal("did not expect empty install path to match a real path")
	}
}
