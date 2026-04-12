package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchVSCodeSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{\"editor.fontSize\":14}\n"), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if err := PatchVSCodeSettings(path, "/usr/local/bin/codex-remote"); err != nil {
		t.Fatalf("patch settings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "\"chatgpt.cliExecutable\": \"/usr/local/bin/codex-remote\"") {
		t.Fatalf("unexpected settings content: %s", text)
	}
}

func TestPatchVSCodeSettingsPreservesJSONCContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := []byte("{\n  // keep existing settings\n  \"editor.fontSize\": 14,\n  \"files.autoSave\": \"afterDelay\",\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := PatchVSCodeSettings(path, "/usr/local/bin/codex-remote"); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	text := string(updated)
	if !strings.Contains(text, "\"editor.fontSize\": 14") {
		t.Fatalf("expected patched settings to preserve editor.fontSize, got %s", text)
	}
	if !strings.Contains(text, "\"files.autoSave\": \"afterDelay\"") {
		t.Fatalf("expected patched settings to preserve files.autoSave, got %s", text)
	}
	if !strings.Contains(text, "\"chatgpt.cliExecutable\": \"/usr/local/bin/codex-remote\"") {
		t.Fatalf("expected patched settings to write cli path, got %s", text)
	}
}

func TestPatchVSCodeSettingsKeepsFileOnMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := []byte("{\n  \"editor.fontSize\": 14,,\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	err := PatchVSCodeSettings(path, "/usr/local/bin/codex-remote")
	if err == nil {
		t.Fatal("expected malformed settings to return error")
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read settings after failure: %v", readErr)
	}
	if string(after) != string(raw) {
		t.Fatalf("expected malformed settings to remain unchanged, got %s", string(after))
	}
}

func TestClearVSCodeSettingsExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := []byte("{\n  \"chatgpt.cliExecutable\": \"/usr/local/bin/codex-remote\",\n  \"editor.fontSize\": 14\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := ClearVSCodeSettingsExecutable(path); err != nil {
		t.Fatalf("clear settings: %v", err)
	}

	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cleared settings: %v", err)
	}
	text := string(updated)
	if strings.Contains(text, "chatgpt.cliExecutable") {
		t.Fatalf("expected cli executable override to be removed, got %s", text)
	}
	if !strings.Contains(text, "\"editor.fontSize\": 14") {
		t.Fatalf("expected unrelated settings to remain, got %s", text)
	}
}

func TestClearVSCodeSettingsExecutableMissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	if err := ClearVSCodeSettingsExecutable(path); err != nil {
		t.Fatalf("clear settings: %v", err)
	}
}

func TestDetectVSCodeSettingsSupportsJSONC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := []byte("{\n  // use wrapper\n  \"chatgpt.cliExecutable\": \"/usr/local/bin/codex-remote\",\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	status, err := DetectVSCodeSettings(path, "/usr/local/bin/codex-remote")
	if err != nil {
		t.Fatalf("detect settings: %v", err)
	}
	if !status.Exists {
		t.Fatalf("expected settings to exist, got %#v", status)
	}
	if status.CLIExecutable != "/usr/local/bin/codex-remote" {
		t.Fatalf("cli executable = %q, want /usr/local/bin/codex-remote", status.CLIExecutable)
	}
	if !status.MatchesBinary {
		t.Fatalf("expected settings to match binary, got %#v", status)
	}
	if status.ParseError != "" {
		t.Fatalf("expected no parse error, got %q", status.ParseError)
	}
}

func TestDetectVSCodeSettingsNormalizesInvalidWindowsPathEscapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	exePath := `C:\Users\demo\bin\codex-remote.exe`
	raw := []byte("{\n  \"chatgpt.cliExecutable\": \"C:\\Users\\demo\\bin\\codex-remote.exe\",\n  \"editor.fontSize\": 14\n}\n")
	// Deliberately degrade to historical invalid JSON escape form: C:\Users\... (single backslashes).
	raw = []byte(strings.ReplaceAll(string(raw), `\\`, `\`))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	status, err := DetectVSCodeSettings(path, exePath)
	if err != nil {
		t.Fatalf("detect settings: %v", err)
	}
	if !status.Exists {
		t.Fatalf("expected settings to exist, got %#v", status)
	}
	if status.CLIExecutable != exePath {
		t.Fatalf("cli executable = %q, want %q", status.CLIExecutable, exePath)
	}
	if !status.MatchesBinary {
		t.Fatalf("expected settings to match binary, got %#v", status)
	}
	if status.ParseError != "" {
		t.Fatalf("expected recoverable invalid escape to be normalized, got parse error %q", status.ParseError)
	}
}

func TestDetectVSCodeSettingsKeepsDetectPathWhenJSONIsUnrecoverable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw := []byte("{\n  \"editor.fontSize\": 14,,\n}\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	status, err := DetectVSCodeSettings(path, "/usr/local/bin/codex-remote")
	if err != nil {
		t.Fatalf("detect settings: %v", err)
	}
	if !status.Exists {
		t.Fatalf("expected settings to exist, got %#v", status)
	}
	if status.ParseError == "" {
		t.Fatalf("expected parse error for unrecoverable malformed JSON, got %#v", status)
	}
	if status.CLIExecutable != "" || status.MatchesBinary {
		t.Fatalf("expected malformed settings to avoid false positive executable match, got %#v", status)
	}
}
