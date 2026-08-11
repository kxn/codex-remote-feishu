package config

import (
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
// connection change carries a generation bump. It consolidates the identical
// validateCodexAPIProfileGenerations and validateOpenCodeAPIProfileGenerations
// implementations.
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

// validateProfileBaseURL verifies that value is an absolute http(s) URL
// without userinfo, query, or fragment. It consolidates the identical
// validateCodexAPIProfileBaseURL and validateOpenCodeAPIProfileBaseURL
// implementations.
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
// for uniqueness. It consolidates the identical codexProfileNameKey and
// opencodeProfileNameKey implementations.
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
	BaseURLOf:  func(p OpenCodeAPIProfileSecretConfig) string { return p.BaseURL },
}
