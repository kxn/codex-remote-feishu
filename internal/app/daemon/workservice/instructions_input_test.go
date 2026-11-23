package workservice

import (
	"errors"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivatePromptKeepsSourceAndBoundedBody(t *testing.T) {
	facts := workPromptFacts{RunRef: "run", Policy: workInputPolicy{ReportingDetail: "normal"}, Inputs: []workPromptInput{{Message: &work.Message{ID: "real-message", BodyRef: "real-body"}, Body: strings.Repeat("甲", 32001)}}}
	prompt, err := renderPrivateWorkInput(facts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "real-message") || !strings.Contains(prompt, "real-body") || !strings.Contains(prompt, "content.read") || strings.Contains(prompt, strings.Repeat("甲", 32001)) || !strings.Contains(prompt, strings.Repeat("甲", 32000)) {
		t.Fatal("private prompt lost source or budget")
	}
	if facts.Inputs[0].BodyTruncated {
		t.Fatal("renderer mutated shared facts")
	}
}

func TestPrivatePromptRejectsOtherPrincipal(t *testing.T) {
	s, attempt := attachmentAttemptPrincipal(t, true, "other-principal")
	if _, err := s.BuildWorkPrompt(t.Context(), attempt, t.TempDir()); !errors.Is(err, workstore.ErrScopeDenied) {
		t.Fatalf("foreign principal accepted: %v", err)
	}
}
