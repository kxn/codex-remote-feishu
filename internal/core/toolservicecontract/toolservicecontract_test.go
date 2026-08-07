package toolservicecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceInfoJSONSchemaStable(t *testing.T) {
	// 状态文件 schema 是跨进程契约：字段名与 JSON tag 必须与历史一致，
	// 任何改动都会破坏已部署 daemon 与 wrapper 的握手。
	info := ServiceInfo{
		URL:         "http://127.0.0.1:9702",
		Protocol:    "mcp",
		Transport:   "streamable_http",
		ManifestURL: "http://127.0.0.1:9702/manifest",
		CallURL:     "http://127.0.0.1:9702/mcp",
		Token:       "secret-token",
		TokenType:   "bearer",
		GeneratedAt: time.Date(2026, 8, 7, 4, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for key, want := range map[string]any{
		"url":         "http://127.0.0.1:9702",
		"protocol":    "mcp",
		"transport":   "streamable_http",
		"manifestUrl": "http://127.0.0.1:9702/manifest",
		"callUrl":     "http://127.0.0.1:9702/mcp",
		"token":       "secret-token",
		"tokenType":   "bearer",
	} {
		if got := decoded[key]; got != want {
			t.Fatalf("json key %q = %v, want %v", key, got, want)
		}
	}
	if _, ok := decoded["generatedAt"]; !ok {
		t.Fatal("json key generatedAt missing")
	}
}

func TestReadServiceInfoRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool-service.json")
	want := ServiceInfo{
		URL:         "http://127.0.0.1:9702",
		Token:       "secret-token",
		TokenType:   "bearer",
		GeneratedAt: time.Now().UTC(),
	}
	if err := WriteJSONFileAtomic(path, want, 0o600); err != nil {
		t.Fatalf("WriteJSONFileAtomic: %v", err)
	}
	got, err := ReadServiceInfo(path)
	if err != nil {
		t.Fatalf("ReadServiceInfo: %v", err)
	}
	if got.URL != want.URL || got.Token != want.Token || got.TokenType != want.TokenType {
		t.Fatalf("ReadServiceInfo = %+v, want %+v", got, want)
	}
}

func TestReadServiceInfoRejectsEmptyPath(t *testing.T) {
	if _, err := ReadServiceInfo("  "); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadServiceInfoMissingFile(t *testing.T) {
	if _, err := ReadServiceInfo(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadServiceInfoLegacySubset(t *testing.T) {
	// 旧版本状态文件只含 url/token/tokenType（wrapper 侧子集 schema），
	// 新 ServiceInfo 读取必须兼容。
	path := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(path, []byte(`{"url":"http://127.0.0.1:9702","token":"t","tokenType":"bearer"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := ReadServiceInfo(path)
	if err != nil {
		t.Fatalf("ReadServiceInfo: %v", err)
	}
	if info.URL != "http://127.0.0.1:9702" || info.Token != "t" || info.TokenType != "bearer" {
		t.Fatalf("legacy parse = %+v", info)
	}
}

func TestWriteJSONFileAtomicRejectsEmptyPath(t *testing.T) {
	if err := WriteJSONFileAtomic("  ", map[string]any{"a": 1}, 0o600); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestWriteJSONFileAtomicWritesIndentedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := WriteJSONFileAtomic(path, map[string]any{"a": 1}, 0o600); err != nil {
		t.Fatalf("WriteJSONFileAtomic: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("expected trailing newline")
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded["a"] != float64(1) {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestErrorPayloadJSONShape(t *testing.T) {
	payload := ErrorPayload{Error: Error{Code: "invalid_token", Message: "missing or invalid bearer token"}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != `{"error":{"code":"invalid_token","message":"missing or invalid bearer token"}}` {
		t.Fatalf("error payload = %s", string(raw))
	}
	var decoded ErrorPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Error.Code != "invalid_token" || decoded.Error.Message != "missing or invalid bearer token" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestCallerInstanceIDQueryParamValue(t *testing.T) {
	if CallerInstanceIDQueryParam != "codex_remote_instance_id" {
		t.Fatalf("CallerInstanceIDQueryParam = %q", CallerInstanceIDQueryParam)
	}
}
