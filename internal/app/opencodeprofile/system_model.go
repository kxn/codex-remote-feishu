package opencodeprofile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

const xdgStateHomeEnv = "XDG_STATE_HOME"

func systemOpenCodeRecentModelForACP(env []string) string {
	hasExplicitModel, reliable := systemOpenCodeConfigHasExplicitModel(env)
	if !reliable || hasExplicitModel {
		return ""
	}
	return systemOpenCodeRecentModel(env)
}

func systemOpenCodeConfigHasExplicitModel(env []string) (bool, bool) {
	configHome := resolveXDGHome(env, config.XDGConfigHomeEnv, ".config")
	if strings.TrimSpace(configHome) == "" {
		return false, false
	}
	raw, err := os.ReadFile(filepath.Join(configHome, "opencode", "opencode.jsonc"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, true
		}
		return false, false
	}
	var doc struct {
		Model string `json:"model"`
	}
	normalized := stripJSONTrailingCommas(stripJSONComments(raw))
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return false, false
	}
	return strings.TrimSpace(doc.Model) != "", true
}

func systemOpenCodeRecentModel(env []string) string {
	stateHome := resolveXDGHome(env, xdgStateHomeEnv, ".local", "state")
	if strings.TrimSpace(stateHome) == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(stateHome, "opencode", "model.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Recent []json.RawMessage `json:"recent"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, recent := range doc.Recent {
		if model := openCodeRecentModelRef(recent); model != "" {
			return model
		}
	}
	return ""
}

func openCodeRecentModelRef(raw json.RawMessage) string {
	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		return normalizeOpenCodeModelRef(stringValue)
	}
	var objectValue struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
		Model      string `json:"model"`
		ID         string `json:"id"`
	}
	if err := json.Unmarshal(raw, &objectValue); err != nil {
		return ""
	}
	if model := normalizeOpenCodeModelRef(objectValue.Model); model != "" {
		return model
	}
	if model := normalizeOpenCodeModelRef(objectValue.ID); model != "" {
		return model
	}
	providerID := strings.TrimSpace(objectValue.ProviderID)
	modelID := strings.TrimSpace(objectValue.ModelID)
	if providerID == "" || modelID == "" {
		return ""
	}
	return normalizeOpenCodeModelRef(providerID + "/" + modelID)
}

func normalizeOpenCodeModelRef(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsFunc(value, unicode.IsSpace) {
		return ""
	}
	providerID, modelID, ok := strings.Cut(value, "/")
	if !ok || strings.TrimSpace(providerID) == "" || strings.TrimSpace(modelID) == "" {
		return ""
	}
	return value
}

func resolveXDGHome(env []string, key string, fallbackParts ...string) string {
	if value := lookupEnvForOpenCodeProfile(env, key); strings.TrimSpace(value) != "" {
		return filepath.Clean(value)
	}
	home := strings.TrimSpace(lookupEnvForOpenCodeProfile(env, "HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(append([]string{home}, fallbackParts...)...)
}

func lookupEnvForOpenCodeProfile(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}

func stripJSONComments(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escape := false
	lineComment := false
	blockComment := false
	for i := 0; i < len(raw); i++ {
		current := raw[i]
		if lineComment {
			if current == '\n' || current == '\r' {
				lineComment = false
				out = append(out, current)
			}
			continue
		}
		if blockComment {
			if current == '\n' || current == '\r' {
				out = append(out, current)
				continue
			}
			if current == '*' && i+1 < len(raw) && raw[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if inString {
			out = append(out, current)
			if escape {
				escape = false
				continue
			}
			switch current {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			out = append(out, current)
			continue
		}
		if current == '/' && i+1 < len(raw) {
			switch raw[i+1] {
			case '/':
				lineComment = true
				i++
				continue
			case '*':
				blockComment = true
				i++
				continue
			}
		}
		out = append(out, current)
	}
	return out
}

func stripJSONTrailingCommas(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escape := false
	for _, current := range raw {
		if inString {
			out = append(out, current)
			if escape {
				escape = false
				continue
			}
			switch current {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
			out = append(out, current)
		case '}', ']':
			for i := len(out) - 1; i >= 0; i-- {
				if isJSONWhitespace(out[i]) {
					continue
				}
				if out[i] == ',' {
					out = append(out[:i], out[i+1:]...)
				}
				break
			}
			out = append(out, current)
		default:
			out = append(out, current)
		}
	}
	return out
}

func isJSONWhitespace(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}
