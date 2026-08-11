package codex

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// translateCompactResumeCWD observes a current thread, then translates a
// compact command targeting another thread, and returns the cwd of the
// resulting thread/resume params. It mirrors the flow used by the existing
// resume tests.
func translateCompactResumeCWD(t *testing.T, currentCWD string, target agentproto.Target) string {
	t.Helper()
	tr := NewTranslator("inst-1")
	observed := `{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"` + currentCWD + `"}}`
	if _, err := tr.ObserveClient([]byte(observed)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: target,
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}
	if len(commands) == 0 {
		t.Fatal("expected commands")
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["method"] != "thread/resume" {
		t.Fatalf("expected thread/resume payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	cwd, _ := params["cwd"].(string)
	return cwd
}

// TestCWDReplayStripsWindowsExtendedPrefixFromTarget verifies a polluted cwd
// carried on the remote command is replayed to codex as a clean native path,
// never as "\\?\" / "//?/" forms.
func TestCWDReplayStripsWindowsExtendedPrefixFromTarget(t *testing.T) {
	cwd := translateCompactResumeCWD(t, "/tmp/current", agentproto.Target{ThreadID: "thread-1", CWD: "//?/C:/repo"})
	if cwd == "//?/C:/repo" || cwd == `\\?\C:\repo` {
		t.Fatalf("polluted cwd replayed to codex: %q", cwd)
	}
	if cwd == "" {
		t.Fatal("expected non-empty replayed cwd")
	}
}

// TestCWDReplayStripsWindowsExtendedPrefixFromObserved verifies a polluted cwd
// observed from a local client (e.g. persisted old state) is normalized when
// stored and replayed clean.
func TestCWDReplayStripsWindowsExtendedPrefixFromObserved(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-current","cwd":"/tmp/current"}}`)); err != nil {
		t.Fatalf("observe current thread: %v", err)
	}
	// Polluted cwd observed for the target thread from a prior local session.
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"//?/C:/repo"}}`)); err != nil {
		t.Fatalf("observe target thread: %v", err)
	}
	// Move the current thread elsewhere so compacting thread-1 needs a resume.
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-2","cwd":"/tmp/two"}}`)); err != nil {
		t.Fatalf("observe third thread: %v", err)
	}
	commands, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandThreadCompactStart,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("translate compact command: %v", err)
	}
	if len(commands) == 0 {
		t.Fatal("expected commands")
	}
	var payload map[string]any
	if err := json.Unmarshal(commands[0], &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["method"] != "thread/resume" {
		t.Fatalf("expected thread/resume payload, got %#v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	cwd, _ := params["cwd"].(string)
	if cwd == "//?/C:/repo" || cwd == `\\?\C:\repo` {
		t.Fatalf("polluted cwd replayed to codex: %q", cwd)
	}
	if cwd == "" {
		t.Fatal("expected non-empty replayed cwd")
	}
}

// TestCWDReplayUsesNativeForm verifies the replayed cwd is the native path
// form expected by the codex process (extended prefix stripped, host-native
// separators).
func TestCWDReplayUsesNativeForm(t *testing.T) {
	cwd := translateCompactResumeCWD(t, "/tmp/current", agentproto.Target{ThreadID: "thread-1", CWD: "//?/C:/repo"})
	if cwd != "C:/repo" && cwd != `C:\repo` {
		t.Fatalf("cwd not native on host: %q", cwd)
	}
}
