package clauderecord

import (
	"encoding/json"
	"testing"
)

func TestMaskSessionID(t *testing.T) {
	entries := []Entry{{
		Seq:       0,
		Direction: DirRecv,
		Frame:     json.RawMessage(`{"type":"system","session_id":"real-session-abc123","model":"claude-sonnet-4-20250514"}`),
	}}
	masked := MaskEntries(entries, MaskOptions{})
	var frame map[string]any
	if err := json.Unmarshal(masked[0].Frame, &frame); err != nil {
		t.Fatal(err)
	}
	if frame["session_id"] != "masked-session-id" {
		t.Errorf("session_id not masked: %v", frame["session_id"])
	}
	if frame["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model should be preserved: %v", frame["model"])
	}
}

func TestMaskUUID(t *testing.T) {
	entries := []Entry{
		{Seq: 0, Direction: DirRecv, Frame: json.RawMessage(`{"uuid":"uuid-aaa"}`)},
		{Seq: 1, Direction: DirRecv, Frame: json.RawMessage(`{"uuid":"uuid-bbb"}`)},
		{Seq: 2, Direction: DirRecv, Frame: json.RawMessage(`{"uuid":"uuid-aaa"}`)},
	}
	masked := MaskEntries(entries, MaskOptions{})

	get := func(i int) string {
		var f map[string]any
		json.Unmarshal(masked[i].Frame, &f)
		return f["uuid"].(string)
	}
	if get(0) != "masked-uuid-1" {
		t.Errorf("first uuid: %s", get(0))
	}
	if get(1) != "masked-uuid-2" {
		t.Errorf("second uuid: %s", get(1))
	}
	// Same original UUID gets same masked value
	if get(2) != "masked-uuid-1" {
		t.Errorf("repeated uuid: %s", get(2))
	}
}

func TestMaskRequestID(t *testing.T) {
	entries := []Entry{
		{Seq: 0, Direction: DirRecv, Frame: json.RawMessage(`{"request_id":"cli-generated-id"}`)},
		{Seq: 1, Direction: DirSend, Frame: json.RawMessage(`{"request_id":"relay-init-0"}`)},
	}
	masked := MaskEntries(entries, MaskOptions{})

	get := func(i int) string {
		var f map[string]any
		json.Unmarshal(masked[i].Frame, &f)
		return f["request_id"].(string)
	}
	if get(0) != "masked-req-1" {
		t.Errorf("cli request_id not masked: %s", get(0))
	}
	// relay-prefixed IDs are kept
	if get(1) != "relay-init-0" {
		t.Errorf("relay request_id should be kept: %s", get(1))
	}
}

func TestMaskPaths(t *testing.T) {
	entries := []Entry{{
		Seq:       0,
		Direction: DirRecv,
		Frame:     json.RawMessage(`{"cwd":"/home/realuser/projects/myapp","file":"/home/realuser/.config/something"}`),
	}}
	masked := MaskEntries(entries, MaskOptions{
		WorkspaceCWD: "/home/realuser/projects/myapp",
		HomePath:     "/home/realuser",
	})
	var frame map[string]any
	if err := json.Unmarshal(masked[0].Frame, &frame); err != nil {
		t.Fatal(err)
	}
	if frame["cwd"] != "/test/workspace" {
		t.Errorf("cwd not masked: %v", frame["cwd"])
	}
	if frame["file"] != "/test/home/.config/something" {
		t.Errorf("home path not masked: %v", frame["file"])
	}
}

func TestMaskAPIKey(t *testing.T) {
	entries := []Entry{{
		Seq:       0,
		Direction: DirRecv,
		Frame:     json.RawMessage(`{"apiKeySource":"env:ANTHROPIC_API_KEY","api_key":"sk-ant-secret"}`),
	}}
	masked := MaskEntries(entries, MaskOptions{})
	var frame map[string]any
	json.Unmarshal(masked[0].Frame, &frame)
	if frame["apiKeySource"] != "REDACTED" {
		t.Errorf("apiKeySource not masked: %v", frame["apiKeySource"])
	}
	if frame["api_key"] != "REDACTED" {
		t.Errorf("api_key not masked: %v", frame["api_key"])
	}
}

func TestMaskNestedStructures(t *testing.T) {
	entries := []Entry{{
		Seq:       0,
		Direction: DirRecv,
		Frame: json.RawMessage(`{
			"type":"control_request",
			"request_id":"cli-req-1",
			"request":{
				"subtype":"mcp_message",
				"server_name":"my-tools",
				"message":{"method":"initialize","id":1}
			}
		}`),
	}}
	masked := MaskEntries(entries, MaskOptions{})
	var frame map[string]any
	json.Unmarshal(masked[0].Frame, &frame)
	if frame["request_id"] != "masked-req-1" {
		t.Errorf("nested request_id not masked: %v", frame["request_id"])
	}
	// Numeric id in MCP message should be preserved
	req := frame["request"].(map[string]any)
	msg := req["message"].(map[string]any)
	if msg["id"] != float64(1) {
		t.Errorf("mcp numeric id should be preserved: %v", msg["id"])
	}
}
