package state

import "strings"

const DefaultCodexProviderID = "default"

func NormalizeCodexProviderID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultCodexProviderID
	}
	return value
}
