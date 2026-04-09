package clauderecord

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskOptions configures which values are replaced during masking.
type MaskOptions struct {
	// WorkspaceCWD is the real CWD to replace with /test/workspace.
	WorkspaceCWD string
	// HomePath is the real home directory to replace with /test/home.
	// If empty, it is inferred from WorkspaceCWD or ignored.
	HomePath string
}

// MaskEntries returns a deep copy of entries with sensitive fields masked.
func MaskEntries(entries []Entry, opts MaskOptions) []Entry {
	m := &masker{
		opts:       opts,
		uuidMap:    map[string]string{},
		requestMap: map[string]string{},
	}
	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = Entry{
			Timestamp: e.Timestamp,
			Direction: e.Direction,
			Seq:       e.Seq,
			Frame:     m.maskFrame(e.Frame),
		}
	}
	return out
}

type masker struct {
	opts       MaskOptions
	uuidMap    map[string]string
	requestMap map[string]string
	uuidSeq    int
	reqSeq     int
}

func (m *masker) maskFrame(raw json.RawMessage) json.RawMessage {
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raw
	}
	masked := m.walkValue(parsed, "")
	out, err := json.Marshal(masked)
	if err != nil {
		return raw
	}
	return out
}

func (m *masker) walkValue(v any, key string) any {
	switch val := v.(type) {
	case map[string]any:
		return m.walkMap(val)
	case []any:
		return m.walkSlice(val)
	case string:
		return m.maskString(val, key)
	default:
		return v
	}
}

func (m *masker) walkMap(obj map[string]any) map[string]any {
	out := make(map[string]any, len(obj))
	for k, v := range obj {
		out[k] = m.walkValue(v, k)
	}
	return out
}

func (m *masker) walkSlice(arr []any) []any {
	out := make([]any, len(arr))
	for i, v := range arr {
		out[i] = m.walkValue(v, "")
	}
	return out
}

func (m *masker) maskString(val string, key string) string {
	// Known sensitive keys
	switch key {
	case "session_id":
		return "masked-session-id"
	case "uuid":
		return m.mapUUID(val)
	case "request_id":
		return m.mapRequestID(val)
	case "api_key", "apiKeySource":
		return "REDACTED"
	case "email":
		return "masked@example.com"
	case "organization":
		return "masked-org"
	}

	// Path masking
	val = m.maskPaths(val)
	return val
}

func (m *masker) maskPaths(val string) string {
	if m.opts.WorkspaceCWD != "" && strings.Contains(val, m.opts.WorkspaceCWD) {
		val = strings.ReplaceAll(val, m.opts.WorkspaceCWD, "/test/workspace")
	}
	if m.opts.HomePath != "" && strings.Contains(val, m.opts.HomePath) {
		val = strings.ReplaceAll(val, m.opts.HomePath, "/test/home")
	}
	return val
}

func (m *masker) mapUUID(original string) string {
	if original == "" {
		return ""
	}
	if mapped, ok := m.uuidMap[original]; ok {
		return mapped
	}
	m.uuidSeq++
	mapped := fmt.Sprintf("masked-uuid-%d", m.uuidSeq)
	m.uuidMap[original] = mapped
	return mapped
}

func (m *masker) mapRequestID(original string) string {
	if original == "" {
		return ""
	}
	// Keep relay-generated IDs (they're not sensitive and aid debugging)
	if strings.HasPrefix(original, "relay-") {
		return original
	}
	if mapped, ok := m.requestMap[original]; ok {
		return mapped
	}
	m.reqSeq++
	mapped := fmt.Sprintf("masked-req-%d", m.reqSeq)
	m.requestMap[original] = mapped
	return mapped
}
