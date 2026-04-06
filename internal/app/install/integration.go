package install

import (
	"fmt"
	"strings"
)

type WrapperIntegrationMode string

const (
	IntegrationEditorSettings WrapperIntegrationMode = "editor_settings"
	IntegrationManagedShim    WrapperIntegrationMode = "managed_shim"
)

func DefaultIntegrations(goos string) []WrapperIntegrationMode {
	switch goos {
	case "linux":
		return []WrapperIntegrationMode{IntegrationEditorSettings, IntegrationManagedShim}
	default:
		return []WrapperIntegrationMode{IntegrationEditorSettings}
	}
}

func ParseIntegrations(raw, goos string) ([]WrapperIntegrationMode, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch trimmed {
	case "", "auto":
		return DefaultIntegrations(goos), nil
	case "both":
		return []WrapperIntegrationMode{IntegrationEditorSettings, IntegrationManagedShim}, nil
	}

	parts := strings.Split(trimmed, ",")
	var values []WrapperIntegrationMode
	for _, part := range parts {
		switch WrapperIntegrationMode(strings.TrimSpace(part)) {
		case IntegrationEditorSettings:
			values = append(values, IntegrationEditorSettings)
		case IntegrationManagedShim:
			values = append(values, IntegrationManagedShim)
		default:
			return nil, fmt.Errorf("unsupported integration mode: %s", part)
		}
	}
	return normalizeIntegrations(values), nil
}

func normalizeIntegrations(values []WrapperIntegrationMode) []WrapperIntegrationMode {
	seen := map[WrapperIntegrationMode]bool{}
	ordered := make([]WrapperIntegrationMode, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		ordered = append(ordered, value)
	}
	return ordered
}

func hasIntegration(values []WrapperIntegrationMode, target WrapperIntegrationMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func integrationsConfigValue(values []WrapperIntegrationMode) string {
	return integrationsConfigValueOr(values, string(IntegrationEditorSettings))
}

func integrationsConfigValueOr(values []WrapperIntegrationMode, emptyValue string) string {
	values = normalizeIntegrations(values)
	if len(values) == 0 {
		return emptyValue
	}
	if len(values) == 2 && hasIntegration(values, IntegrationEditorSettings) && hasIntegration(values, IntegrationManagedShim) {
		return "both"
	}
	return string(values[0])
}
