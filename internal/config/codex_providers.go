package config

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	CodexDefaultProviderID   = "default"
	CodexDefaultProviderName = "系统默认"

	CodexProviderAPIKeyEnv    = "CODEX_REMOTE_CODEX_PROVIDER_API_KEY"
	CodexRuntimeProviderIDEnv = "CODEX_REMOTE_CODEX_PROVIDER_ID"
)

type CodexSettings struct {
	Providers []CodexProviderConfig `json:"providers,omitempty"`
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
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func CodexProviderIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if id := CanonicalCodexProviderID(name); id != "" {
		return id
	}
	sum := sha1.Sum([]byte(name))
	return "provider-" + hex.EncodeToString(sum[:])[:12]
}

func NormalizeCodexProviderNameForReservedCheck(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), ""))
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

func IsReservedCodexProviderID(value string) bool {
	return CanonicalCodexProviderID(value) == "openai"
}

func IsReservedCodexProviderName(name string) bool {
	return NormalizeCodexProviderNameForReservedCheck(name) == "openai"
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
			APIKey:          strings.TrimSpace(provider.APIKey),
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
	for _, provider := range NormalizeCodexProviders(cfg.Codex.Providers) {
		providers = append(providers, CodexProvider{CodexProviderConfig: provider})
	}
	return providers
}

func IndexOfCodexProvider(providers []CodexProviderConfig, providerID string) int {
	providerID = CanonicalCodexProviderID(providerID)
	if providerID == "" || providerID == CodexDefaultProviderID {
		return -1
	}
	for index, provider := range providers {
		if CanonicalCodexProviderID(provider.ID) == providerID {
			return index
		}
	}
	return -1
}

func ResolveCodexProvider(cfg AppConfig, providerID string) (CodexProvider, bool) {
	providerID = NormalizeCodexProviderID(providerID)
	if providerID == CodexDefaultProviderID {
		return BuiltInCodexProvider(), true
	}
	for _, provider := range NormalizeCodexProviders(cfg.Codex.Providers) {
		if provider.ID == providerID {
			return CodexProvider{CodexProviderConfig: provider}, true
		}
	}
	return CodexProvider{}, false
}

func PrepareCodexProviderCreate(existing []CodexProviderConfig, requested CodexProviderConfig) (CodexProviderConfig, error) {
	return prepareCodexProviderConfig(existing, "", requested)
}

func PrepareCodexProviderUpdate(existing []CodexProviderConfig, currentID string, requested CodexProviderConfig) (CodexProviderConfig, error) {
	return prepareCodexProviderConfig(existing, currentID, requested)
}

func ValidateCodexProviderConfig(provider CodexProviderConfig) error {
	rawReasoningEffort := strings.TrimSpace(provider.ReasoningEffort)
	provider = CodexProviderConfig{
		ID:              strings.TrimSpace(provider.ID),
		Name:            strings.TrimSpace(provider.Name),
		BaseURL:         strings.TrimSpace(provider.BaseURL),
		APIKey:          strings.TrimSpace(provider.APIKey),
		Model:           strings.TrimSpace(provider.Model),
		ReasoningEffort: NormalizeCodexReasoningEffort(provider.ReasoningEffort),
	}
	if rawReasoningEffort != "" && provider.ReasoningEffort == "" {
		return fmt.Errorf("codex provider reasoningEffort is invalid")
	}
	if provider.Name == "" {
		return fmt.Errorf("codex provider name is required")
	}
	if provider.BaseURL == "" {
		return fmt.Errorf("codex provider baseURL is required")
	}
	if provider.APIKey == "" {
		return fmt.Errorf("codex provider apiKey is required")
	}
	if IsBuiltInCodexProviderID(provider.ID) {
		return fmt.Errorf("the built-in default codex provider cannot be replaced")
	}
	if IsReservedCodexProviderID(provider.ID) {
		return fmt.Errorf("codex provider id %q is reserved", provider.ID)
	}
	if IsReservedCodexProviderName(provider.Name) {
		return fmt.Errorf("codex provider name %q is reserved", provider.Name)
	}
	return nil
}

func CodexProviderLaunchOverrides(provider CodexProvider) []string {
	if provider.BuiltIn {
		return nil
	}
	overrides := []string{
		"-c", codexProviderOverride("model_provider", provider.ID),
		"-c", codexProviderOverride("model_providers."+provider.ID+".name", provider.Name),
		"-c", codexProviderOverride("model_providers."+provider.ID+".base_url", provider.BaseURL),
		"-c", codexProviderOverride("model_providers."+provider.ID+".env_key", CodexProviderAPIKeyEnv),
	}
	if value := strings.TrimSpace(provider.Model); value != "" {
		overrides = append(overrides, "-c", codexProviderOverride("model", value))
	}
	if value := NormalizeCodexReasoningEffort(provider.ReasoningEffort); value != "" {
		overrides = append(overrides, "-c", codexProviderOverride("model_reasoning_effort", value))
	}
	return overrides
}

func codexProviderOverride(key, value string) string {
	return strings.TrimSpace(key) + "=" + strconv.Quote(strings.TrimSpace(value))
}

func prepareCodexProviderConfig(existing []CodexProviderConfig, currentID string, requested CodexProviderConfig) (CodexProviderConfig, error) {
	currentID = CanonicalCodexProviderID(currentID)
	normalizedExisting := NormalizeCodexProviders(existing)
	currentIndex := -1
	current := CodexProviderConfig{}
	if currentID != "" {
		currentIndex = IndexOfCodexProvider(normalizedExisting, currentID)
		if currentIndex < 0 {
			return CodexProviderConfig{}, fmt.Errorf("codex provider %q not found", currentID)
		}
		current = normalizedExisting[currentIndex]
	}

	name := strings.TrimSpace(requested.Name)
	providerID := CanonicalCodexProviderID(requested.ID)
	if providerID == "" {
		providerID = CodexProviderIDFromName(name)
	}
	provider := CodexProviderConfig{
		ID:              providerID,
		Name:            name,
		BaseURL:         strings.TrimSpace(requested.BaseURL),
		APIKey:          strings.TrimSpace(requested.APIKey),
		Model:           strings.TrimSpace(requested.Model),
		ReasoningEffort: strings.TrimSpace(requested.ReasoningEffort),
	}
	if currentID != "" && provider.APIKey == "" {
		provider.APIKey = strings.TrimSpace(current.APIKey)
	}
	if err := ValidateCodexProviderConfig(provider); err != nil {
		return CodexProviderConfig{}, err
	}
	provider.ReasoningEffort = NormalizeCodexReasoningEffort(provider.ReasoningEffort)
	if existingIndex := IndexOfCodexProvider(normalizedExisting, provider.ID); existingIndex >= 0 && existingIndex != currentIndex {
		return CodexProviderConfig{}, fmt.Errorf("codex provider id %q already exists", provider.ID)
	}
	return provider, nil
}

func nextCodexProviderID(id, name string, used map[string]struct{}) string {
	base := CanonicalCodexProviderID(chooseNonEmpty(id, name, "provider"))
	if base == "" {
		base = "provider"
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
