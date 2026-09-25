package feishu

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestProjectFinalModelSummary(t *testing.T) {
	for _, tc := range []struct {
		name, cwd, model, effort string
		worktree                 *gitWorktreeSummary
		want                     string
	}{
		{name: "no cwd", model: "runtime-model", effort: "high", want: "**模型** <text_tag color='neutral'>runtime-model</text_tag> <text_tag color='neutral'>high</text_tag>"},
		{name: "outside git", cwd: "/tmp/outside", model: "runtime-model", want: "**模型** <text_tag color='neutral'>runtime-model</text_tag>"},
		{name: "unknown", effort: "high"},
		{name: "clean", cwd: "/tmp/repo", model: "runtime-model", worktree: &gitWorktreeSummary{}, want: "**模型** <text_tag color='neutral'>runtime-model</text_tag>  **工作区** <text_tag color='neutral'>干净</text_tag>"},
		{name: "dirty", cwd: "/tmp/repo", model: "runtime-model", worktree: &gitWorktreeSummary{Dirty: true, ModifiedCount: 1, Files: []string{"file.go"}}, want: "**模型** <text_tag color='neutral'>runtime-model</text_tag> **工作区** <text_tag color='neutral'>有改动</text_tag> <text_tag color='neutral'>1修改</text_tag> <text_tag color='neutral'>file.go</text_tag>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewProjector()
			p.readGitWorktree = func(string) *gitWorktreeSummary { return tc.worktree }
			ops := p.ProjectEvent("chat-1", eventcontract.Event{Kind: eventcontract.KindBlockCommitted, SourceMessageID: "msg-1", Block: &render.Block{Kind: render.BlockAssistantMarkdown, Text: "done", Final: true}, FinalTurnSummary: &control.FinalTurnSummary{Elapsed: time.Second, ThreadCWD: tc.cwd, Model: tc.model, ReasoningEffort: tc.effort}})
			if len(ops) != 1 || ops[0].Kind != OperationSendCard {
				t.Fatalf("unexpected ops: %#v", ops)
			}
			if tc.want == "" {
				for _, elem := range ops[0].CardElements {
					if strings.Contains(elem["content"].(string), "**模型**") {
						t.Fatalf("unexpected model footer: %#v", elem)
					}
				}
			} else if len(ops[0].CardElements) != 2 || ops[0].CardElements[1]["content"] != tc.want {
				t.Fatalf("footer = %#v, want %q", ops[0].CardElements, tc.want)
			}
		})
	}
}
