package codexprofile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOAuthAccountFingerprintUsesFullIdentityAndRealm(t *testing.T) {
	first := OAuthProbeEvidence{AccountType: "chatgpt", AuthMethod: "chatgpt", Email: "user-one@example.com"}
	second := first
	second.Email = "user-two@example.com"
	a, b := ClassifyOAuthProbe(first), ClassifyOAuthProbe(second)
	if a.AccountHint != b.AccountHint || a.AccountFingerprint == "" || a.AccountFingerprint == b.AccountFingerprint {
		t.Fatal("redacted hint used as cache identity")
	}
	realm := first
	realm.ChatGPTBaseURL = "https://other.example/backend-api/"
	if ClassifyOAuthProbe(realm).AccountFingerprint == a.AccountFingerprint {
		t.Fatal("different account realm shared identity")
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), first.Email) {
		t.Fatal("fingerprint retained full email")
	}
	first.PlanType = "pro"
	if ClassifyOAuthProbe(first).AccountFingerprint != a.AccountFingerprint {
		t.Fatal("plan edit changed account identity")
	}
	first.Email = ""
	if ClassifyOAuthProbe(first).AccountFingerprint != "" {
		t.Fatal("missing account identity fabricated cache identity")
	}
	first.Email = "u***@example.com"
	if ClassifyOAuthProbe(first).AccountFingerprint != "" {
		t.Fatal("masked native email fabricated account identity")
	}
	first.AccountID = "opaque-account"
	if ClassifyOAuthProbe(first).AccountFingerprint == "" {
		t.Fatal("account ID not accepted as actual identity")
	}
}
