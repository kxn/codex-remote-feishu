package config

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	legacyCodexDefaultProviderID = "default"
	CodexRuntimeProfileIDEnv     = "CODEX_REMOTE_CODEX_PROFILE_ID"
)

type CodexSettings struct {
	DefaultModel                   string                            `json:"defaultModel,omitempty"`
	DefaultReasoningEffort         string                            `json:"defaultReasoningEffort,omitempty"`
	ProfileCatalogMigrationVersion int                               `json:"profileCatalogMigrationVersion,omitempty"`
	MigrationDiagnostics           []CodexProfileMigrationDiagnostic `json:"profileCatalogMigrationDiagnostics,omitempty"`
	Profiles                       []CodexAPIProfileRecord           `json:"profiles,omitempty"`
	// Providers is a load-only legacy migration input; WriteAppConfig omits it.
	Providers []LegacyCodexProviderConfig `json:"providers,omitempty"`
}

func ValidateCodexRemoteDefault(model, effort string) error {
	model, effort = strings.TrimSpace(model), strings.TrimSpace(effort)
	if model == "" && effort == "" {
		return nil
	}
	if model == "" || effort == "" || len(model) > 128 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return fmt.Errorf("default model and reasoning effort must both be valid or both be empty")
	}
	switch effort {
	case "low", "medium", "high", "xhigh", "max":
		return nil
	default:
		return fmt.Errorf("unsupported default reasoning effort %q", effort)
	}
}

type LegacyCodexProviderConfig struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	APIKey          string `json:"apiKey,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

func canonicalLegacyCodexProviderID(value string) string {
	return canonicalProfileID(value, '-')
}

func NormalizeCodexReasoningEffort(value string) string {
	effort := strings.ToLower(strings.TrimSpace(value))
	switch effort {
	case "low", "medium", "high", "xhigh":
		return effort
	default:
		return ""
	}
}

func NormalizeLegacyCodexProviders(providers []LegacyCodexProviderConfig) []LegacyCodexProviderConfig {
	if len(providers) == 0 {
		return nil
	}
	normalized := make([]LegacyCodexProviderConfig, 0, len(providers))
	used := map[string]struct{}{
		legacyCodexDefaultProviderID: {},
	}
	for _, provider := range providers {
		current := LegacyCodexProviderConfig{
			ID:              strings.TrimSpace(provider.ID),
			Name:            strings.TrimSpace(provider.Name),
			BaseURL:         strings.TrimSpace(provider.BaseURL),
			APIKey:          provider.APIKey,
			Model:           strings.TrimSpace(provider.Model),
			ReasoningEffort: NormalizeCodexReasoningEffort(provider.ReasoningEffort),
		}
		current.ID = nextLegacyCodexProviderID(current.ID, current.Name, used)
		if current.Name == "" {
			current.Name = current.ID
		}
		normalized = append(normalized, current)
	}
	return normalized
}

func nextLegacyCodexProviderID(id, name string, used map[string]struct{}) string {
	return nextCatalogID(canonicalLegacyCodexProviderID, "provider", id, name, used)
}
