package codexoauthstate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
)

func TestAccountFingerprintPersistsAcrossRestartWithoutWeakeningExecutionGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth.json")
	store := NewStore(path)
	observation := func(email string) codexprofile.OAuthProbeObservation {
		return codexprofile.OAuthProbeObservation{Result: codexprofile.ClassifyOAuthProbe(codexprofile.OAuthProbeEvidence{AccountType: "chatgpt", AuthMethod: "chatgpt", Email: email, PlanType: "plus"}), CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}
	}
	first, _, err := store.ApplyProbe(observation("user-one@example.com"), time.Now(), "daemon-1", false)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := reloaded.ApplyProbe(observation("user-one@example.com"), time.Now(), "daemon-2", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.AccountFingerprint == "" || second.AccountFingerprint != first.AccountFingerprint || second.AuthGeneration != first.AuthGeneration+1 {
		t.Fatal("restart lost cache identity or weakened execution generation")
	}
	third, _, err := reloaded.ApplyProbe(observation("user-two@example.com"), time.Now(), "daemon-2", false)
	if err != nil {
		t.Fatal(err)
	}
	if third.AccountHint != second.AccountHint || third.AccountFingerprint == second.AccountFingerprint || third.AuthGeneration != second.AuthGeneration+1 {
		t.Fatal("same-redacted-hint account switch merged identities")
	}
	noIdentity := observation("")
	missing, _, err := reloaded.ApplyProbe(noIdentity, time.Now(), "daemon-2", false)
	if err != nil {
		t.Fatal(err)
	}
	if missing.AccountFingerprint != "" {
		t.Fatal("missing account identity retained previous account key")
	}
}
