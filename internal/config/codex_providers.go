package config

import (
	"strings"
)

const (
	CodexDefaultProviderID   = "default"
	CodexDefaultProviderName = "系统默认"

	CodexProviderAPIKeyEnv    = "CODEX_REMOTE_CODEX_PROVIDER_API_KEY"
	CodexRuntimeProviderIDEnv = "CODEX_REMOTE_CODEX_PROVIDER_ID"
)

type CodexSettings struct {
	ProfileCatalogMigrationVersion int                               `json:"profileCatalogMigrationVersion,omitempty"`
	MigrationDiagnostics           []CodexProfileMigrationDiagnostic `json:"profileCatalogMigrationDiagnostics,omitempty"`
	Profiles                       []CodexAPIProfileRecord           `json:"profiles,omitempty"`
	Providers                      []CodexProviderConfig             `json:"providers,omitempty"`
}

type CodexProviderConfig struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	BaseURL         string `json:"baseURL,omitempty"`
	APIKey          string `json:"apiKey,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

type CodexProvider struct {
	CodexProviderConfig
	BuiltIn bool
}

func BuiltInCodexProvider() CodexProvider {
	return CodexProvider{
		BuiltIn: true,
		CodexProviderConfig: CodexProviderConfig{
			ID:   CodexDefaultProviderID,
			Name: CodexDefaultProviderName,
		},
	}
}

func CanonicalCodexProviderID(value string) string {
	return canonicalProfileID(value, '-')
}

func NormalizeCodexProviderID(value string) string {
	value = CanonicalCodexProviderID(value)
	if value == "" {
		return CodexDefaultProviderID
	}
	return value
}

func IsBuiltInCodexProviderID(value string) bool {
	return NormalizeCodexProviderID(value) == CodexDefaultProviderID
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

func NormalizeCodexProviders(providers []CodexProviderConfig) []CodexProviderConfig {
	if len(providers) == 0 {
		return nil
	}
	normalized := make([]CodexProviderConfig, 0, len(providers))
	used := map[string]struct{}{
		CodexDefaultProviderID: {},
	}
	for _, provider := range providers {
		current := CodexProviderConfig{
			ID:              strings.TrimSpace(provider.ID),
			Name:            strings.TrimSpace(provider.Name),
			BaseURL:         strings.TrimSpace(provider.BaseURL),
			APIKey:          provider.APIKey,
			Model:           strings.TrimSpace(provider.Model),
			ReasoningEffort: NormalizeCodexReasoningEffort(provider.ReasoningEffort),
		}
		current.ID = nextCodexProviderID(current.ID, current.Name, used)
		if current.Name == "" {
			current.Name = current.ID
		}
		normalized = append(normalized, current)
	}
	return normalized
}

func ListCodexProviders(cfg AppConfig) []CodexProvider {
	providers := []CodexProvider{BuiltInCodexProvider()}
	if len(cfg.Codex.Profiles) > 0 {
		for _, record := range NormalizeCodexAPIProfileRecords(cfg.Codex.Profiles) {
			profile, ok := CurrentCodexAPIProfile(record)
			if !ok {
				continue
			}
			providers = append(providers, CodexProvider{CodexProviderConfig: codexProviderConfigFromAPIProfile(profile)})
		}
		return providers
	}
	for _, provider := range NormalizeCodexProviders(cfg.Codex.Providers) {
		providers = append(providers, CodexProvider{CodexProviderConfig: provider})
	}
	return providers
}

func codexProviderConfigFromAPIProfile(profile CodexAPIProfileSecretConfig) CodexProviderConfig {
	return CodexProviderConfig{
		ID:              profile.ID,
		Name:            profile.Name,
		BaseURL:         profile.BaseURL,
		APIKey:          profile.APIKey,
		Model:           profile.Model,
		ReasoningEffort: profile.ReasoningEffort,
	}
}

func nextCodexProviderID(id, name string, used map[string]struct{}) string {
	return nextCatalogID(CanonicalCodexProviderID, "provider", id, name, used)
}
