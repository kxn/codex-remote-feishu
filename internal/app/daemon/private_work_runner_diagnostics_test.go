package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

func TestPrivateRunnerDiagnosticsKeepsOriginalCause(t *testing.T) {
	var diagnostics privateRunnerDiagnostics
	now := time.Now()
	for _, expected := range []error{
		privatechat.ErrDeferred,
		privatechat.ErrDisplayHeld,
		errPrivatePublicationUnknown,
		fmt.Errorf("wrapper: %w", errors.Join(privatechat.ErrDeferred, context.Canceled)),
	} {
		if got := diagnostics.message(now, "effect", "old", expected); got != "" {
			t.Errorf("expected wait was logged: %s", got)
		}
	}
	sendErr := errors.Join(errPrivatePublicationUnknown, errors.New("controlled Feishu timeout"))
	first := diagnostics.message(now, "effect", "old", sendErr)
	if !strings.Contains(first, `stage=effect effect="old"`) || !strings.Contains(first, "controlled Feishu timeout") {
		t.Fatalf("publication error lost its context or cause: %s", first)
	}
	if got := diagnostics.message(now.Add(time.Second), "effect", "old", sendErr); got != "" {
		t.Errorf("repeated failure logged again: %s", got)
	}
	if got := diagnostics.message(now, "effect", "old", nil); got != "" {
		t.Errorf("success was logged: %s", got)
	}
	if got := diagnostics.message(now, "effect", "old", sendErr); got != first {
		t.Errorf("new failure after success did not log the original cause: %s", got)
	}
	passErr := fmt.Errorf("load next page: %w", privatechat.ErrJournalConflict)
	if got := diagnostics.message(now, "pass", "", passErr); !strings.Contains(got, "load next page: "+privatechat.ErrJournalConflict.Error()) {
		t.Errorf("pass error lost stage or cause: %s", got)
	}
}

func TestPrivateRunnerDiagnosticsBoundsUniqueFailures(t *testing.T) {
	var diagnostics privateRunnerDiagnostics
	now := time.Now()
	err := errors.New("controlled journal read failure")
	for i := 0; i <= privateRunnerDiagnosticBudget; i++ {
		got := diagnostics.message(now, "effect", fmt.Sprintf("effect-%d", i), err)
		if (got != "") != (i < privateRunnerDiagnosticBudget) {
			t.Fatalf("unexpected log budget at effect %d: %q", i, got)
		}
	}
	for i := 0; i <= privateRunnerDiagnosticHistory; i++ {
		now = now.Add(time.Minute)
		got := diagnostics.message(now, "effect", fmt.Sprintf("later-effect-%d", i), err)
		if got == "" {
			t.Fatalf("distinct failure suppressed after budget reset at effect %d", i)
		}
	}
	if got := len(diagnostics.recent); got != privateRunnerDiagnosticHistory {
		t.Fatalf("unexpected failure history size: %d", got)
	}
}
