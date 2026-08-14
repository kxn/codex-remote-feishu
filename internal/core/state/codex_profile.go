package state

import "strings"

const (
	DefaultCodexProfileID        = NativeCodexProfileID
	LegacyDefaultCodexProviderID = "default"
	DefaultCodexProfileName      = "本机默认"
)

func NormalizeCodexProfileID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, LegacyDefaultCodexProviderID) {
		return DefaultCodexProfileID
	}
	return value
}
