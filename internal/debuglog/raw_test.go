package debuglog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRawLoggerWritesNDJSONWithStructuredFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.ndjson")
	logger, err := OpenRaw(path, "wrapper", "inst-1", 42)
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer logger.Close()

	logger.Log(RawEntry{
		Channel:      "codex.stdout",
		Direction:    "in",
		EnvelopeType: "command",
		CommandID:    "cmd-1",
		Frame:        []byte("{\"id\":\"cmd-1\",\"method\":\"turn/started\"}\n"),
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v\nraw=%s", err, raw)
	}
	if payload["component"] != "wrapper" {
		t.Fatalf("component = %#v", payload["component"])
	}
	if payload["instanceId"] != "inst-1" {
		t.Fatalf("instanceId = %#v", payload["instanceId"])
	}
	if payload["channel"] != "codex.stdout" || payload["direction"] != "in" {
		t.Fatalf("unexpected channel/direction: %#v", payload)
	}
	frame, ok := payload["frame"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured frame, got %#v", payload["frame"])
	}
	if frame["method"] != "turn/started" {
		t.Fatalf("frame = %#v", frame)
	}
}

func TestRawLoggerFallsBackToTextForInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.ndjson")
	logger, err := OpenRaw(path, "daemon", "", 7)
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer logger.Close()

	logger.Log(RawEntry{
		Channel:   "relay.ws",
		Direction: "in",
		Frame:     []byte("not-json\n"),
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal: %v\nraw=%s", err, raw)
	}
	if payload["text"] != "not-json" {
		t.Fatalf("text = %#v", payload["text"])
	}
	if _, ok := payload["frame"]; ok {
		t.Fatalf("did not expect frame for invalid json: %#v", payload["frame"])
	}
}

func TestRawLoggerRedactsSensitiveJSONFrameValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.ndjson")
	logger, err := OpenRaw(path, "wrapper", "inst-1", 42)
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer logger.Close()

	logger.Log(RawEntry{
		Channel:   "codex.stdin",
		Direction: "out",
		Frame: []byte(`{
			"method": "session/new",
			"params": {
				"mcpServers": [{
					"type": "http",
					"name": "codex_remote_feishu",
					"url": "http://127.0.0.1:9702/mcp",
					"headers": [{"name": "Authorization", "value": "Bearer secret-token"}]
				}],
				"auth": {"key": "profile-api-key", "accessToken": "access-token"}
			}
		}`),
	})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) == "" {
		t.Fatal("expected raw log content")
	}
	if stringContainsAny(string(raw), "secret-token", "profile-api-key", "access-token") {
		t.Fatalf("raw log leaked secret: %s", raw)
	}
	if !strings.Contains(string(raw), "redacted") {
		t.Fatalf("raw log did not include redaction marker: %s", raw)
	}
}

func stringContainsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
