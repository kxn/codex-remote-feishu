package state

import "strings"

const (
	DefaultCodexProviderID   = "default"
	DefaultCodexProviderName = "系统默认"
)

type CodexProviderRecord struct {
	ID      string
	Name    string
	BuiltIn bool
}

func NormalizeCodexProviderID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DefaultCodexProviderID
	}
	return value
}

func NormalizeCodexProviderRecord(value CodexProviderRecord) CodexProviderRecord {
	value.ID = NormalizeCodexProviderID(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	if value.Name == "" {
		if value.ID == DefaultCodexProviderID {
			value.Name = DefaultCodexProviderName
		} else {
			value.Name = value.ID
		}
	}
	if value.ID == DefaultCodexProviderID {
		value.BuiltIn = true
	}
	return value
}
