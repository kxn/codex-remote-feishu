package wrapper

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeBackendRuntimeRejectsMissingExplicitTargetBeforeTranslation(t *testing.T) {
	for _, current := range []string{"", "session-current"} {
		for _, fork := range []bool{false, true} {
			t.Run(current+map[bool]string{false: "/resume", true: "/fork"}[fork], func(t *testing.T) {
				t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
				runtime := &claudeBackendRuntime{workspaceRoot: t.TempDir(), translator: claude.NewTranslator("inst-1")}
				if current != "" {
					if _, err := runtime.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session-current","permissionMode":"default"}`)); err != nil {
						t.Fatal(err)
					}
				}
				target := agentproto.Target{ThreadID: "session-missing"}
				code := "claude_resume_thread_not_found"
				if fork {
					target = agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeForkEphemeral, SourceThreadID: "session-missing", Purpose: agentproto.PromptPurposeReview}
					code = "claude_fork_source_thread_not_found"
				}
				result, err := runtime.TranslateCommand(agentproto.Command{
					CommandID: "cmd-missing", Kind: agentproto.CommandPromptSend, Target: target,
					Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "belongs to another work"}}},
				})
				var problem agentproto.ErrorInfo
				if !errors.As(err, &problem) || problem.Code != code || problem.ThreadID != "session-missing" {
					t.Fatalf("error = %#v, want %s for missing target", err, code)
				}
				if result.Restart != nil || len(result.Phases) != 0 || len(result.Events) != 0 {
					t.Fatalf("rejected command produced runtime effects: %#v", result)
				}
				if got := runtime.translator.RuntimeStateSnapshot().SessionID; got != current {
					t.Fatalf("current session changed to %q, want %q", got, current)
				}
				// A failed target must not leave a pending turn behind to consume later native output.
				observed, err := runtime.ObserveServer([]byte(`{"type":"result","subtype":"success","result":"unrelated completion"}`))
				if err != nil {
					t.Fatal(err)
				}
				for _, event := range observed.Events {
					if event.CommandID == "cmd-missing" {
						t.Fatalf("rejected command acquired native output: %#v", event)
					}
				}
			})
		}
	}
}

func TestClaudeBackendRuntimeCurrentSessionDoesNotRequireCatalogEntry(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	runtime := &claudeBackendRuntime{workspaceRoot: t.TempDir(), translator: claude.NewTranslator("inst-1")}
	if _, err := runtime.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session-current","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.TranslateCommand(agentproto.Command{
		CommandID: "cmd-current", Kind: agentproto.CommandPromptSend,
		Target: agentproto.Target{ThreadID: "session-current"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "continue"}}},
	})
	if err != nil || result.Restart != nil || len(result.Phases) != 1 {
		t.Fatalf("current observed session: restart=%v phases=%d, error=%v", result.Restart != nil, len(result.Phases), err)
	}
}
