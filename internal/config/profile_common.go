package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/text/cases"
)

// apiGenerationAccessors exposes the fields of an API profile secret config
// needed by validateProfileGenerations. Accessor functions keep the generic
// core decoupled from the concrete secret config structs.
type apiGenerationAccessors[T any] struct {
	RevisionOf func(T) uint64
	CredGenOf  func(T) uint64
	ConnGenOf  func(T) uint64
	APIKeyOf   func(T) string
	BaseURLOf  func(T) string
}

// validateProfileGenerations verifies that credential/connection generations
// across an ordered revision list are monotonic and that every credential or
// connection change carries a generation bump across profile types.
func validateProfileGenerations[T any](revisions []T, access apiGenerationAccessors[T]) error {
	ordered := append([]T{}, revisions...)
	sort.Slice(ordered, func(left, right int) bool {
		return access.RevisionOf(ordered[left]) < access.RevisionOf(ordered[right])
	})
	for index := 1; index < len(ordered); index++ {
		previous := ordered[index-1]
		current := ordered[index]
		revisionDelta := access.RevisionOf(current) - access.RevisionOf(previous)
		if access.CredGenOf(current) < access.CredGenOf(previous) ||
			access.CredGenOf(current)-access.CredGenOf(previous) > revisionDelta ||
			access.ConnGenOf(current) < access.ConnGenOf(previous) ||
			access.ConnGenOf(current)-access.ConnGenOf(previous) > revisionDelta {
			return fmt.Errorf("generation is not monotonic")
		}
		credentialChanged := access.CredGenOf(current) != access.CredGenOf(previous)
		if access.APIKeyOf(current) != access.APIKeyOf(previous) && !credentialChanged {
			return fmt.Errorf("credential changed without generation")
		}
		if (access.BaseURLOf(current) != access.BaseURLOf(previous) || credentialChanged) && access.ConnGenOf(current) == access.ConnGenOf(previous) {
			return fmt.Errorf("connection changed without generation")
		}
	}
	return nil
}

// canonicalProfileID lowercases and trims value, keeps [a-z0-9] runes, and
// folds every other rune into a single separator.
func canonicalProfileID(value string, separator byte) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastSeparator := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastSeparator = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastSeparator = false
		default:
			if builder.Len() > 0 && !lastSeparator {
				builder.WriteByte(separator)
				lastSeparator = true
			}
		}
	}
	return strings.Trim(builder.String(), string(separator))
}

// nextCatalogID derives a unique catalog id from id/name using the given
// canonicalizer, appending -2, -3, ... on collisions.
func nextCatalogID(canonical func(string) string, fallback, id, name string, used map[string]struct{}) string {
	base := canonical(chooseNonEmpty(id, name, fallback))
	if base == "" {
		base = fallback
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

// newRandomProfileID generates a random 128-bit profile id with the given
// prefix that is not present in used.
func newRandomProfileID(name, idPrefix string, used map[string]struct{}) (string, error) {
	for attempt := 0; attempt < 4; attempt++ {
		raw := make([]byte, 16)
		if _, err := rand.Read(raw); err != nil {
			return "", fmt.Errorf("generate %s profile id: %w", name, err)
		}
		candidate := idPrefix + hex.EncodeToString(raw)
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generate unique %s profile id", name)
}

// validateProfileBaseURL verifies that value is an absolute http(s) URL
// without userinfo, query, or fragment.
func validateProfileBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("absolute http(s) URL without userinfo, query, or fragment is required")
	}
	return nil
}

// profileNameKey returns the case-folded key used to compare profile names
// for uniqueness.
func profileNameKey(name string) string {
	return cases.Fold().String(strings.TrimSpace(name))
}

var codexGenerationAccessors = apiGenerationAccessors[CodexAPIProfileSecretConfig]{
	RevisionOf: func(p CodexAPIProfileSecretConfig) uint64 { return p.Revision },
	CredGenOf:  func(p CodexAPIProfileSecretConfig) uint64 { return p.CredentialGeneration },
	ConnGenOf:  func(p CodexAPIProfileSecretConfig) uint64 { return p.ConnectionGeneration },
	APIKeyOf:   func(p CodexAPIProfileSecretConfig) string { return p.APIKey },
	BaseURLOf:  func(p CodexAPIProfileSecretConfig) string { return p.BaseURL },
}

var openCodeGenerationAccessors = apiGenerationAccessors[OpenCodeAPIProfileSecretConfig]{
	RevisionOf: func(p OpenCodeAPIProfileSecretConfig) uint64 { return p.Revision },
	CredGenOf:  func(p OpenCodeAPIProfileSecretConfig) uint64 { return p.CredentialGeneration },
	ConnGenOf:  func(p OpenCodeAPIProfileSecretConfig) uint64 { return p.ConnectionGeneration },
	APIKeyOf:   func(p OpenCodeAPIProfileSecretConfig) string { return p.APIKey },
	BaseURLOf: func(p OpenCodeAPIProfileSecretConfig) string {
		return normalizeOpenCodeProviderType(p.ProviderType) + "\x00" + strings.TrimSpace(p.BaseURL)
	},
}
