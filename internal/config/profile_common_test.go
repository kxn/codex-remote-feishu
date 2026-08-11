package config

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func TestCanonicalProfileIDFoldsSeparators(t *testing.T) {
	for _, tc := range []struct {
		name      string
		separator byte
		want      string
	}{
		{name: "  Team++Proxy__01  ", separator: '-', want: "team-proxy-01"},
		{name: "  Team++Proxy__01  ", separator: '_', want: "team_proxy_01"},
		{name: "!!!", separator: '-', want: ""},
	} {
		if got := canonicalProfileID(tc.name, tc.separator); got != tc.want {
			t.Fatalf("canonicalProfileID(%q, %q) = %q, want %q", tc.name, tc.separator, got, tc.want)
		}
	}
}

func TestNextCatalogIDUsesFallbackAndCollisionSuffix(t *testing.T) {
	canonical := func(value string) string {
		return canonicalProfileID(value, '-')
	}
	used := map[string]struct{}{
		"profile":      {},
		"team-proxy":   {},
		"team-proxy-2": {},
	}

	if got := nextCatalogID(canonical, "profile", "", "Team Proxy", used); got != "team-proxy-3" {
		t.Fatalf("nextCatalogID() = %q, want team-proxy-3", got)
	}
	if got := nextCatalogID(canonical, "profile", "", "!!!", used); got != "profile-2" {
		t.Fatalf("nextCatalogID(fallback) = %q, want profile-2", got)
	}
}

func TestValidateProfileBaseURLRejectsUnsafeParts(t *testing.T) {
	for _, value := range []string{
		"https://proxy.example/v1",
		"http://localhost:11434/v1",
	} {
		if err := validateProfileBaseURL(value); err != nil {
			t.Fatalf("validateProfileBaseURL(%q): %v", value, err)
		}
	}

	for _, value := range []string{
		"proxy.example/v1",
		"ftp://proxy.example/v1",
		"https://user:pass@proxy.example/v1",
		"https://proxy.example/v1?token=secret",
		"https://proxy.example/v1#fragment",
	} {
		if err := validateProfileBaseURL(value); err == nil {
			t.Fatalf("validateProfileBaseURL(%q) accepted an unsafe URL", value)
		}
	}
}

func TestNewRandomProfileIDRetriesCollisions(t *testing.T) {
	oldReader := rand.Reader
	t.Cleanup(func() {
		rand.Reader = oldReader
	})

	collisionBytes := bytes.Repeat([]byte{0x11}, 16)
	uniqueBytes := bytes.Repeat([]byte{0x22}, 16)
	rand.Reader = bytes.NewReader(append(append([]byte{}, collisionBytes...), uniqueBytes...))

	used := map[string]struct{}{
		"tp_" + strings.Repeat("11", 16): {},
	}
	id, err := newRandomProfileID("test", "tp_", used)
	if err != nil {
		t.Fatalf("newRandomProfileID: %v", err)
	}
	if want := "tp_" + strings.Repeat("22", 16); id != want {
		t.Fatalf("newRandomProfileID() = %q, want %q", id, want)
	}
}
