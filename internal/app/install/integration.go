package install

import (
	"fmt"
	"strings"
)

type WrapperIntegrationMode string

const (
	IntegrationManagedShim WrapperIntegrationMode = "managed_shim"
)

func DefaultIntegrations(goos string) []WrapperIntegrationMode {
	return []WrapperIntegrationMode{IntegrationManagedShim}
}

func ParseIntegrations(raw, goos string) ([]WrapperIntegrationMode, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch trimmed {
	case "", "auto":
		return DefaultIntegrations(goos), nil
	}

	parts := strings.Split(trimmed, ",")
	var values []WrapperIntegrationMode
	for _, part := range parts {
		switch WrapperIntegrationMode(strings.TrimSpace(part)) {
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
		switch strings.TrimSpace(string(value)) {
		case string(IntegrationManagedShim):
			value = IntegrationManagedShim
		default:
			continue
		}
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
	return integrationsConfigValueOr(values, string(IntegrationManagedShim))
}

func integrationsConfigValueOr(values []WrapperIntegrationMode, emptyValue string) string {
	values = normalizeIntegrations(values)
	if len(values) == 0 {
		return emptyValue
	}
	return string(values[0])
}
