package acp

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

func TestPathWithinWorkspaceRootWindowsExtendedForms(t *testing.T) {
	cases := []struct {
		name, root, target string
		want               bool
	}{
		{"drive descendant", `C:\repo`, `C:\repo\pkg`, true},
		{"drive self", `C:\repo`, `C:\repo`, true},
		{"drive sibling", `C:\repo`, `C:\repo2`, false},
		{"extended drive", `\\?\C:\repo`, `//?/C:/repo/pkg`, true},
		{"extended UNC", `\\?\UNC\server\share`, `//?/UNC/server/share/repo`, true},
		{"UNC outside", `\\server\share`, `\\other\share`, false},
		{"case-insensitive", `C:\Repo`, `c:/repo/pkg`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithinWorkspaceRoot(tc.root, tc.target); got != tc.want {
				t.Fatalf("pathWithinWorkspaceRoot(%q, %q) = %v, want %v", tc.root, tc.target, got, tc.want)
			}
		})
	}
}

func TestResolveWorkspaceWritePathNativeCWD(t *testing.T) {
	// On the host, a native workspace root must resolve write paths without
	// leaking extended-length or slash-mixed forms into the relative result.
	root := t.TempDir()
	targetAbs, rel, err := resolveWorkspaceWritePath(root, filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatalf("resolveWorkspaceWritePath() error = %v", err)
	}
	if rel != "notes.txt" {
		t.Fatalf("resolveWorkspaceWritePath() rel = %q, want notes.txt", rel)
	}
	if pathcanon.Native(targetAbs) != filepath.Clean(filepath.Join(root, "notes.txt")) {
		t.Fatalf("resolveWorkspaceWritePath() targetAbs = %q", targetAbs)
	}
}

func TestPromptSendCanonicalizesWindowsExtendedCWD(t *testing.T) {
	tr := NewTranslator("inst-1", `\\?\C:\repo`)
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandPromptSend,
		Target: agentproto.Target{
			ExecutionMode: agentproto.PromptExecutionModeStartNew,
			CWD:           `//?/C:/repo`,
		},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	params := asMap(t, frame["params"])
	if params["cwd"] != `C:\repo` {
		t.Fatalf("session/new cwd = %#v, want native extended-prefix-free path", params["cwd"])
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result":  map[string]any{"sessionId": "ses_1"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(new response): %v", err)
	}
	assertEventKinds(t, observed.Events,
		agentproto.EventThreadDiscovered,
		agentproto.EventThreadFocused,
		agentproto.EventTurnStarted,
	)
	for _, event := range observed.Events {
		if event.CWD != `C:\repo` {
			t.Fatalf("event %s cwd = %q, want native extended-prefix-free path", event.Kind, event.CWD)
		}
	}
}

func TestSessionListCanonicalizesWindowsExtendedCWD(t *testing.T) {
	tr := NewTranslator("inst-1", `\\?\C:\repo`)
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-list",
		Kind:      agentproto.CommandThreadsRefresh,
		Target:    agentproto.Target{CWD: `//?/C:/repo`},
	})
	if err != nil {
		t.Fatalf("TranslateCommand: %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	params := asMap(t, frame["params"])
	if params["cwd"] != `C:\repo` {
		t.Fatalf("session/list cwd = %#v, want native extended-prefix-free path", params["cwd"])
	}

	observed, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"result": map[string]any{
			"sessions": []any{
				map[string]any{"sessionId": "ses_1", "cwd": `//?/C:/repo`, "title": "Repo"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(list response): %v", err)
	}
	assertEventKinds(t, observed.Events, agentproto.EventThreadsSnapshot)
	if got := observed.Events[0].Threads[0].CWD; got != `C:\repo` {
		t.Fatalf("snapshot cwd = %q, want native extended-prefix-free path", got)
	}
}
