package debuglog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RawRecord struct {
	Timestamp    time.Time       `json:"ts"`
	Component    string          `json:"component"`
	PID          int             `json:"pid,omitempty"`
	InstanceID   string          `json:"instanceId,omitempty"`
	Channel      string          `json:"channel"`
	Direction    string          `json:"direction"`
	EnvelopeType string          `json:"envelopeType,omitempty"`
	CommandID    string          `json:"commandId,omitempty"`
	Frame        json.RawMessage `json:"frame,omitempty"`
	Text         string          `json:"text,omitempty"`
}

type RawEntry struct {
	InstanceID   string
	Channel      string
	Direction    string
	EnvelopeType string
	CommandID    string
	Frame        []byte
}

type RawLogger struct {
	component  string
	instanceID string
	pid        int

	mu   sync.Mutex
	file *os.File
}

func OpenRaw(path, component, instanceID string, pid int) (*RawLogger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &RawLogger{
		component:  component,
		instanceID: instanceID,
		pid:        pid,
		file:       file,
	}, nil
}

func (l *RawLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *RawLogger) Log(entry RawEntry) {
	if l == nil || l.file == nil {
		return
	}
	record := RawRecord{
		Timestamp:    time.Now().UTC(),
		Component:    l.component,
		PID:          l.pid,
		InstanceID:   chooseFirstNonEmpty(entry.InstanceID, l.instanceID),
		Channel:      entry.Channel,
		Direction:    entry.Direction,
		EnvelopeType: entry.EnvelopeType,
		CommandID:    entry.CommandID,
	}
	frame := bytesTrimSpace(entry.Frame)
	if len(frame) > 0 {
		if json.Valid(frame) {
			record.Frame = redactRawJSONFrame(frame)
		} else {
			record.Text = string(frame)
		}
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	payload = append(payload, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	_, _ = l.file.Write(payload)
}

func bytesTrimSpace(raw []byte) []byte {
	start := 0
	for start < len(raw) {
		switch raw[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			goto trimEnd
		}
	}
trimEnd:
	end := len(raw)
	for end > start {
		switch raw[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return raw[start:end]
		}
	}
	return raw[start:end]
}

func redactRawJSONFrame(frame []byte) json.RawMessage {
	var payload any
	if err := json.Unmarshal(frame, &payload); err != nil {
		return json.RawMessage(frame)
	}
	redacted, err := json.Marshal(redactRawJSONValue(payload))
	if err != nil {
		return json.RawMessage(frame)
	}
	return json.RawMessage(redacted)
}

func redactRawJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		name := ""
		if rawName, ok := typed["name"].(string); ok {
			name = rawName
		}
		for key, item := range typed {
			if key == "value" && isSensitiveRawField(name) {
				out[key] = "<redacted>"
				continue
			}
			if isSensitiveRawField(key) {
				out[key] = "<redacted>"
				continue
			}
			out[key] = redactRawJSONValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactRawJSONValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveRawField(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(normalized)
	if normalized == "key" || normalized == "authorization" || normalized == "proxyauthorization" {
		return true
	}
	for _, marker := range []string{"token", "secret", "password", "credential", "apikey", "bearer"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func chooseFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
