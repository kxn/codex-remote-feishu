package orchestrator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newReviewSessionService(t *testing.T) (*Service, *state.SurfaceConsoleRecord, string) {
	t.Helper()
	now := time.Date(2026, 4, 26, 15, 0, 0, 0, time.UTC)
	repoRoot := initReviewSessionRepo(t)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           repoRoot,
		WorkspaceKey:            repoRoot,
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {
				ThreadID: "thread-main",
				Name:     "主线程",
				CWD:      repoRoot,
				Loaded:   true,
			},
			"thread-review": {
				ThreadID:     "thread-review",
				Name:         "审阅线程",
				CWD:          repoRoot,
				Loaded:       true,
				ForkedFromID: "thread-main",
				Source: &agentproto.ThreadSourceRecord{
					Kind:           agentproto.ThreadSourceKindReview,
					Name:           "review",
					ParentThreadID: "thread-main",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-main",
	})
	return svc, svc.root.Surfaces["surface-1"], repoRoot
}

func initReviewSessionRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	runReviewSessionGit(t, root, "init")
	runReviewSessionGit(t, root, "config", "user.email", "review-session-tests@example.com")
	runReviewSessionGit(t, root, "config", "user.name", "Review Session Tests")
	return root
}

func runReviewSessionGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func writeReviewSessionRepoFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", fullPath, err)
	}
}

func commitReviewSessionRepoFile(t *testing.T, root, relativePath, content, message string) gitmeta.CommitSummary {
	t.Helper()
	writeReviewSessionRepoFile(t, root, relativePath, content)
	runReviewSessionGit(t, root, "add", relativePath)
	runReviewSessionGit(t, root, "-c", "user.name=review-session-tests", "-c", "user.email=review-session-tests@example.com", "commit", "-q", "-m", message)
	commits, err := gitmeta.ListRecentCommits(root, 1)
	if err != nil {
		t.Fatalf("ListRecentCommits(%q): %v", root, err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected latest commit after %q, got %#v", message, commits)
	}
	return commits[0]
}

func fakeReviewSessionGitLogMarkerForTest(t *testing.T, marker string) string {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not available")
	}
	t.Setenv("REVIEW_SESSION_REAL_GIT_FOR_TEST", realGit)
	bin := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(bin, "git.bat")
		body := "@echo off\r\n" +
			"if \"%1\"==\"log\" echo invoked>>\"" + marker + "\"\r\n" +
			"\"%REVIEW_SESSION_REAL_GIT_FOR_TEST%\" %*\r\n" +
			"exit /b %ERRORLEVEL%\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		return bin
	}
	path := filepath.Join(bin, "git")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"log\" ]; then printf 'invoked\\n' >> " + shellQuoteForReviewSessionTest(marker) + "; fi\n" +
		"exec \"$REVIEW_SESSION_REAL_GIT_FOR_TEST\" \"$@\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return bin
}

func shellQuoteForReviewSessionTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func activateReviewSessionForTest(t *testing.T, svc *Service, surface *state.SurfaceConsoleRecord, sourceMessageID, turnID string) {
	t.Helper()
	if surface == nil {
		t.Fatal("expected surface")
	}
	if surface.ReviewSession == nil {
		surface.ReviewSession = &state.ReviewSessionRecord{}
	}
	surface.ReviewSession.Phase = state.ReviewSessionPhasePending
	surface.ReviewSession.ParentThreadID = "thread-main"
	surface.ReviewSession.ReviewThreadID = "thread-review"
	surface.ReviewSession.SourceMessageID = sourceMessageID
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-review",
		TurnID:    turnID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	for _, event := range events {
		if event.ThreadSelection != nil {
			t.Fatalf("expected review turn start not to steal selection, got %#v", events)
		}
	}
}

func TestReviewSessionTurnStartActivatesWithoutStealingSelection(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)

	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	session := surface.ReviewSession
	if session == nil || session.Phase != state.ReviewSessionPhaseActive {
		t.Fatalf("expected active review session, got %#v", session)
	}
	if session.ParentThreadID != "thread-main" || session.ReviewThreadID != "thread-review" || session.InitialTurnID != "turn-review-1" || session.ActiveTurnID != "turn-review-1" {
		t.Fatalf("unexpected review session runtime: %#v", session)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected parent thread selection to remain pinned, got %q", surface.SelectedThreadID)
	}
	inst := svc.root.Instances["inst-1"]
	if inst.ActiveThreadID != "thread-review" || inst.ActiveTurnID != "turn-review-1" {
		t.Fatalf("expected instance active turn to follow review thread, got thread=%q turn=%q", inst.ActiveThreadID, inst.ActiveTurnID)
	}
}

func TestReviewSessionLateThreadSourceUpdateActivatesPendingSessionAndRoutesFinal(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	inst := svc.root.Instances["inst-1"]
	inst.Threads["thread-review"] = &state.ThreadRecord{
		ThreadID: "thread-review",
		Name:     "审阅线程",
		Preview:  "审阅中",
		CWD:      repoRoot,
		Loaded:   true,
	}
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		ParentThreadID:  "thread-main",
		SourceMessageID: "msg-review-start",
	}

	earlyEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-review",
		Name:     "审阅线程",
		Preview:  "审阅中",
		CWD:      repoRoot,
		Metadata: map[string]any{
			"forkedFromId": "thread-main",
		},
	})
	if len(earlyEvents) != 0 {
		t.Fatalf("expected early unclassified review thread discovery to stay quiet, got %#v", earlyEvents)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhasePending {
		t.Fatalf("expected unclassified thread discovery to keep pending session, got %#v", surface.ReviewSession)
	}

	metadataEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventThreadDiscovered,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata: map[string]any{
			"forkedFromId": "thread-main",
			"threadSource": &agentproto.ThreadSourceRecord{
				Kind:           agentproto.ThreadSourceKindReview,
				Name:           "review",
				ParentThreadID: "thread-main",
			},
		},
	})
	if len(metadataEvents) != 0 {
		t.Fatalf("expected late review metadata update to stay quiet, got %#v", metadataEvents)
	}
	if surface.ReviewSession == nil ||
		surface.ReviewSession.Phase != state.ReviewSessionPhaseActive ||
		surface.ReviewSession.ParentThreadID != "thread-main" ||
		surface.ReviewSession.ReviewThreadID != "thread-review" ||
		surface.ReviewSession.ActiveTurnID != "turn-review-1" {
		t.Fatalf("expected late review metadata to activate pending review session, got %#v", surface.ReviewSession)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected late review metadata not to steal parent selection, got %q", surface.SelectedThreadID)
	}

	itemEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "No findings."},
	})
	if len(itemEvents) != 0 {
		t.Fatalf("expected completed agent message to wait for final render, got %#v", itemEvents)
	}

	finalEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	var finalBlock *render.Block
	for i := range finalEvents {
		if finalEvents[i].Block != nil && finalEvents[i].Block.Final {
			finalBlock = finalEvents[i].Block
		}
	}
	if finalBlock == nil || finalBlock.Text != "No findings." {
		t.Fatalf("expected final review block on surface, got %#v", finalEvents)
	}
}

func TestReviewSessionTextRecoversReviewThreadSelection(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "初始审阅意见"},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.releaseSurfaceThreadClaim(surface)
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-review") {
		t.Fatal("expected test surface to claim review thread")
	}
	surface.SelectedThreadID = "thread-review"
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "这里需要继续审阅",
	})

	if len(events) != 3 || events[2].Command == nil {
		t.Fatalf("expected review session command after recovery, got %#v", events)
	}
	command := events[2].Command
	if command.Target.ThreadID != "thread-review" ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected recovered review session command target: %#v", command.Target)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected review text recovery to restore parent selection, got %q", surface.SelectedThreadID)
	}
}

func TestReviewSessionFinalRenderDoesNotStealSelection(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	itemEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "建议继续收口 review session 的状态机。"},
	})
	if len(itemEvents) != 0 {
		t.Fatalf("expected completed agent message to wait for final render, got %#v", itemEvents)
	}

	finalEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected review final render to keep parent selection, got %q with events %#v", surface.SelectedThreadID, finalEvents)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.ActiveTurnID != "" {
		t.Fatalf("expected idle active review session after final render, got %#v", surface.ReviewSession)
	}
	if svc.ReviewSession(surface.SurfaceSessionID) == nil {
		t.Fatalf("expected review session to remain visible for final-card exit actions")
	}
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	replyEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "再按这个方向检查一遍",
	})
	if len(replyEvents) != 3 || replyEvents[2].Command == nil {
		t.Fatalf("expected follow-up review prompt command, got %#v", replyEvents)
	}
	command := replyEvents[2].Command
	if command.Target.ThreadID != "thread-review" ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected follow-up review target: %#v", command.Target)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected follow-up review prompt to keep parent selection, got %q", surface.SelectedThreadID)
	}
}

func TestDetachedReviewTurnCompletionMakesResultReadyForParentApply(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "建议修复 detached review 的结果状态。"},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-compaction",
		ItemKind:  "context_compaction",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	session := surface.ReviewSession
	if session == nil || session.Phase != state.ReviewSessionPhaseReady || session.ActiveTurnID != "" {
		t.Fatalf("expected ready detached review session, got %#v", session)
	}
	if session.LastReviewText != "建议修复 detached review 的结果状态。" {
		t.Fatalf("unexpected detached review result: %#v", session)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	var command *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = event.Command
			break
		}
	}
	if command == nil {
		t.Fatalf("expected ready review result to dispatch to parent, got %#v", events)
	}
	if command.Target.ThreadID != "thread-main" || len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != reviewApplyPromptPrefix+"建议修复 detached review 的结果状态。" {
		t.Fatalf("unexpected detached review apply command: %#v", command)
	}
}

func TestDetachedReviewFollowUpDoesNotReplaceReadyReviewResult(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	surface.ReviewSession.LastReviewText = "初始审阅意见"
	surface.ReviewSession.Phase = state.ReviewSessionPhaseReady
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	followUpEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	if noticeCode(followUpEvents, "review_follow_up_waiting_text") == "" {
		t.Fatalf("expected follow-up capture prompt, got %#v", followUpEvents)
	}
	followUpEvents = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-follow-up",
		Text:             "现在改好了吗",
	})
	if len(followUpEvents) != 3 || followUpEvents[2].Command == nil {
		t.Fatalf("expected follow-up prompt on review thread, got %#v", followUpEvents)
	}
	blockedBeforeTurnStart := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-follow-up-dispatching",
	})
	if noticeCode(blockedBeforeTurnStart, "review_turn_active") == "" {
		t.Fatalf("expected apply to wait while review follow-up is dispatching, got %#v", blockedBeforeTurnStart)
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-2",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-follow-up-running",
	})
	if noticeCode(blocked, "review_turn_active") == "" {
		t.Fatalf("expected apply to wait for active review follow-up, got %#v", blocked)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-2",
		ItemID:    "follow-up-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "还没有，这只是一次追问。"},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-2",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhaseReady || surface.ReviewSession.LastReviewText != "初始审阅意见" {
		t.Fatalf("expected follow-up completion to preserve ready review result, got %#v", surface.ReviewSession)
	}
}

func TestDetachedReviewFollowUpLifecycleDoesNotReplaceReadyReviewResult(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	surface.ReviewSession.LastReviewText = "初始审阅意见"
	surface.ReviewSession.Phase = state.ReviewSessionPhaseReady
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-follow-up",
		Text:             "再检查一次",
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-2",
		ItemID:    "review-exit-2",
		ItemKind:  "exited_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "追问结果不能成为应用意见"},
	})

	if surface.ReviewSession == nil || surface.ReviewSession.LastReviewText != "初始审阅意见" {
		t.Fatalf("expected lifecycle follow-up to preserve initial review text, got %#v", surface.ReviewSession)
	}
}

func TestFailedInitialReviewClearsSessionInsteadOfLeavingDeadOverlay(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-review",
		TurnID:       "turn-review-1",
		Status:       "failed",
		ErrorMessage: "review failed",
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	if surface.ReviewSession != nil {
		t.Fatalf("failed initial review must clear the blocking overlay, got %#v", surface.ReviewSession)
	}
}

func TestNewThreadExitsIdleReviewSessionBeforeFirstText(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	newEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	if surface.ReviewSession != nil {
		t.Fatalf("expected /new to exit idle review session, got %#v", surface.ReviewSession)
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected /new to prepare a fresh thread route, got %#v", surface)
	}
	if len(newEvents) == 0 || newEvents[len(newEvents)-1].Notice == nil || newEvents[len(newEvents)-1].Notice.Code != "new_thread_ready" {
		t.Fatalf("expected new_thread_ready notice, got %#v", newEvents)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-new",
		Text:             "你好",
	})

	if len(events) != 3 {
		t.Fatalf("expected queue-on, queue-off, and prompt command, got %#v", events)
	}
	command := events[2].Command
	if command == nil || command.Kind != agentproto.CommandPromptSend {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	if command.Target.ThreadID != "" ||
		command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		!command.Target.CreateThreadIfMissing ||
		command.Target.SurfaceBindingPolicy == agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("expected first post-/new text to create a fresh thread, got %#v", command.Target)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil ||
		queuedItemExecutionThreadID(item) != "" ||
		queuedItemPromptDispatchPlan(item).ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		t.Fatalf("expected queued item to freeze new-thread creation, got %#v", item)
	}
}

func TestNewThreadBlockedWhileReviewTurnRunning(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: surface.SurfaceSessionID,
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "new_thread_blocked_review_running" {
		t.Fatalf("expected running review to block /new, got %#v", events)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.ActiveTurnID != "turn-review-1" {
		t.Fatalf("expected running review session to remain active, got %#v", surface.ReviewSession)
	}
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected blocked /new to preserve parent selection, got %#v", surface)
	}
}

func TestReviewSessionLifecycleAndReplyAnchorFallback(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	entered := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-enter",
		ItemKind:  "entered_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "未提交变更"},
	})
	if len(entered) != 0 {
		t.Fatalf("expected entered review lifecycle item to stay internal, got %#v", entered)
	}
	if surface.ReviewSession.TargetLabel != "未提交变更" {
		t.Fatalf("expected review target label to persist, got %#v", surface.ReviewSession)
	}

	exited := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-exit",
		ItemKind:  "exited_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "建议把 review/start 的 translator 回归测试补齐"},
	})
	if len(exited) != 0 {
		t.Fatalf("expected exited review lifecycle item to stay internal, got %#v", exited)
	}
	if surface.ReviewSession.LastReviewText != "建议把 review/start 的 translator 回归测试补齐" {
		t.Fatalf("expected review result to persist on session, got %#v", surface.ReviewSession)
	}

	requestEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		RequestID: "req-review-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(requestEvents) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", requestEvents)
	}
	if requestEvents[0].SourceMessageID != "msg-review-start" {
		t.Fatalf("expected review request to reuse session reply anchor, got %#v", requestEvents[0])
	}
	if requestEvents[0].RequestView == nil || requestEvents[0].RequestView.TemporarySessionLabel != reviewTemporarySessionLabel {
		t.Fatalf("expected review request prompt to carry temporary session label, got %#v", requestEvents[0])
	}
	record := surface.PendingRequests["req-review-1"]
	if record == nil || record.SourceMessageID != "msg-review-start" || record.ThreadID != "thread-review" {
		t.Fatalf("expected pending request to stay bound to review session surface, got %#v", record)
	}
	if svc.turnSurface("inst-1", "thread-review", "turn-review-1") != surface {
		t.Fatalf("expected review thread turnSurface fallback to resolve surface")
	}
}

func TestReviewSessionLifecycleActivatesPendingSessionWithoutRemoteTurnOwnership(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		SourceMessageID: "msg-review-start",
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-enter",
		ItemKind:  "entered_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "未提交变更"},
	})
	if len(events) != 0 {
		t.Fatalf("expected entered review lifecycle item to stay internal, got %#v", events)
	}
	session := surface.ReviewSession
	if session == nil || session.Phase != state.ReviewSessionPhaseActive {
		t.Fatalf("expected lifecycle item to activate pending review session, got %#v", session)
	}
	if session.ParentThreadID != "thread-main" || session.ReviewThreadID != "thread-review" || session.ActiveTurnID != "turn-review-1" {
		t.Fatalf("unexpected activated review session runtime: %#v", session)
	}
	if session.TargetLabel != "未提交变更" {
		t.Fatalf("expected review target label to persist, got %#v", session)
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-exit",
		ItemKind:  "exited_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "初始审阅意见"},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	replyEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "这里继续看一下",
	})
	if len(replyEvents) != 3 || replyEvents[2].Command == nil {
		t.Fatalf("expected review reply command after lifecycle activation, got %#v", replyEvents)
	}
	command := replyEvents[2].Command
	if command.Target.ThreadID != "thread-review" ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected review reply command target: %#v", command.Target)
	}
}

func TestReviewSessionFinalBlockCarriesTemporarySessionLabel(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	itemEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-result",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"text": "建议把 review 前台投影接回统一 temporary session。"},
	})
	if len(itemEvents) != 0 {
		t.Fatalf("expected completed agent message to wait for final render, got %#v", itemEvents)
	}

	finalEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	var finalBlock *render.Block
	for i := range finalEvents {
		if finalEvents[i].Block != nil && finalEvents[i].Block.Final {
			finalBlock = finalEvents[i].Block
		}
	}
	if finalBlock == nil {
		t.Fatalf("expected final review block, got %#v", finalEvents)
	}
	if finalBlock.TemporarySessionLabel != reviewTemporarySessionLabel {
		t.Fatalf("expected review final block label, got %#v", finalBlock)
	}
}

func TestReviewCommandExecutionProgressShowsSharedProgressInNormalVerbosity(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.Verbosity = state.SurfaceVerbosityNormal
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-review",
		TurnID:   "turn-review-1",
		ItemID:   "cmd-review-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "git show --stat --oneline HEAD~1..HEAD",
			"cwd":     "/data/dl/droid",
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected review command execution progress in normal verbosity, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if progress.TemporarySessionLabel != reviewTemporarySessionLabel {
		t.Fatalf("expected review progress label, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "command_execution" {
		t.Fatalf("expected command execution timeline item, got %#v", progress.Timeline)
	}
}

func TestStartReviewFromFinalCardBuildsDetachedReviewCommand(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	finalBlock := render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}
	svc.RecordFinalCardMessage(surface.SurfaceSessionID, finalBlock, "msg-user-1", "om-final-1", "life-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewStart,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-final-1",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start target: %#v", command.Target)
	}
	if command.Review.Delivery != agentproto.ReviewDeliveryDetached || command.Review.Target.Kind != agentproto.ReviewTargetKindUncommittedChanges {
		t.Fatalf("unexpected review request: %#v", command.Review)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhasePending || surface.ReviewSession.ParentThreadID != "thread-main" || surface.ReviewSession.SourceMessageID != "om-final-1" {
		t.Fatalf("unexpected pending review session: %#v", surface.ReviewSession)
	}
}

func TestStartReviewRespectsRoomConcurrencyBeforeCreatingPendingSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)
	other := addActiveReviewSurfaceForTest(svc, room)
	room.ActiveReservations["queue:other"] = &state.FeishuRoomActiveReservationRecord{
		ReservationID:    "queue:other",
		SurfaceSessionID: other.SurfaceSessionID,
		Reason:           "review_running",
	}

	events := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if noticeCode(events, "room_workspace_active") == "" {
		t.Fatalf("expected review start to respect room admission, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("room admission failure must not create pending review session: %#v", surface.ReviewSession)
	}
}

func TestReviewStartReservationReleasesAfterReviewTurnCompletes(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)

	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 || len(room.ActiveReservations) != 1 {
		t.Fatalf("expected review start reservation, events=%#v reservations=%#v", startEvents, room.ActiveReservations)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("review reservation count after terminal turn = %d, want 0", got)
	}
}

func TestReviewStartDispatchFailureReleasesReservationAndPendingSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)

	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 || surface.ReviewSession == nil || len(room.ActiveReservations) != 1 {
		t.Fatalf("expected pending review start, events=%#v session=%#v reservations=%#v", startEvents, surface.ReviewSession, room.ActiveReservations)
	}

	const commandID = "cmd-review-start-dispatch-failure"
	svc.BindPendingReviewStartCommand(surface.SurfaceSessionID, commandID)
	events := svc.HandleCommandDispatchFailure(surface.SurfaceSessionID, commandID, errors.New("relay unavailable"))
	if noticeCode(events, "dispatch_failed") == "" {
		t.Fatalf("expected review dispatch failure notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected failed review start to clear pending session, got %#v", surface.ReviewSession)
	}
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected failed review start to release reservation, got %d", got)
	}
}

func TestReviewStartCommandRejectReleasesReservationAndPendingSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)

	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 || surface.ReviewSession == nil || len(room.ActiveReservations) != 1 {
		t.Fatalf("expected pending review start, events=%#v session=%#v reservations=%#v", startEvents, surface.ReviewSession, room.ActiveReservations)
	}

	const commandID = "cmd-review-start-rejected"
	svc.BindPendingReviewStartCommand(surface.SurfaceSessionID, commandID)
	events := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: commandID,
		Problem: &agentproto.ErrorInfo{
			Code:    "review_start_rejected",
			Message: "review start rejected",
		},
	})
	if noticeCode(events, "command_rejected") == "" {
		t.Fatalf("expected review command rejection notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected rejected review start to clear pending session, got %#v", surface.ReviewSession)
	}
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected rejected review start to release reservation, got %d", got)
	}
}

func feishuReviewRoomForTest(t *testing.T, svc *Service, surface *state.SurfaceConsoleRecord) *state.FeishuRoomContextRecord {
	t.Helper()
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)
	return room
}

func addActiveReviewSurfaceForTest(svc *Service, room *state.FeishuRoomContextRecord) *state.SurfaceConsoleRecord {
	surface := &state.SurfaceConsoleRecord{
		SurfaceSessionID: "feishu:app-2:chat:" + room.ChatID,
		GatewayID:        "app-2",
		ChatID:           room.ChatID,
		ActorUserID:      "ou_other",
		ReviewSession: &state.ReviewSessionRecord{
			Phase: state.ReviewSessionPhaseActive,
		},
	}
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	svc.ensureFeishuRoomContextForSurface(surface)
	return surface
}

func TestReviewReservationReconcileDropsReservationAfterSessionClears(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	room := feishuReviewRoomForTest(t, svc, surface)
	room.ActiveReservations["review:stale"] = &state.FeishuRoomActiveReservationRecord{
		ReservationID:    "review:stale",
		SurfaceSessionID: surface.SurfaceSessionID,
		Reason:           "review_running",
	}

	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected stale review reservation to be reconciled away, got %d", got)
	}
}

func TestReviewReservationReconcileDropsReservationAfterSurfaceIsRemoved(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	room := feishuReviewRoomForTest(t, svc, surface)
	room.ActiveReservations["review:missing-surface"] = &state.FeishuRoomActiveReservationRecord{
		ReservationID:    "review:missing-surface",
		SurfaceSessionID: surface.SurfaceSessionID,
		Reason:           "review_running",
	}
	delete(svc.root.Surfaces, surface.SurfaceSessionID)

	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected review reservation for removed surface to be reconciled away, got %d", got)
	}
}

func TestPendingReviewProblemReleasesReservationAfterCommandAccepted(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	room := feishuReviewRoomForTest(t, svc, surface)
	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 {
		t.Fatalf("expected review start events, got %#v", startEvents)
	}
	const commandID = "cmd-review-start-accepted"
	svc.BindPendingReviewStartCommand(surface.SurfaceSessionID, commandID)
	svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: commandID, Accepted: true})

	events := svc.HandleProblem("inst-1", agentproto.ErrorInfo{
		Code:             "review_start_failed",
		Layer:            "codex",
		Stage:            "review_start",
		Message:          "review start failed",
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	if len(events) == 0 {
		t.Fatalf("expected review failure notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected pending review session to clear after failure, got %#v", surface.ReviewSession)
	}
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected review failure to release reservation, got %d", got)
	}
}

func TestReviewReservationReleasesOnTransportDegraded(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	room := feishuReviewRoomForTest(t, svc, surface)
	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 {
		t.Fatalf("expected review start events, got %#v", startEvents)
	}

	svc.ApplyInstanceTransportDegraded("inst-1", false)
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected transport degraded to release review reservation, got %d", got)
	}
}

func TestReviewReservationReleasesOnDisconnect(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	room := feishuReviewRoomForTest(t, svc, surface)
	startEvents := svc.startReview(surface, reviewStartState{
		Ready:           true,
		ParentThreadID:  "thread-main",
		ThreadCWD:       svc.root.Instances["inst-1"].WorkspaceRoot,
		SourceMessageID: "msg-review",
		Target:          agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges},
	})
	if len(startEvents) != 2 {
		t.Fatalf("expected review start events, got %#v", startEvents)
	}

	svc.ApplyInstanceDisconnected("inst-1")
	if got := svc.feishuRoomActiveReservationCount(room); got != 0 {
		t.Fatalf("expected disconnect to release review reservation, got %d", got)
	}
}

func TestReviewApplyKeepsSessionWhenRoomAdmissionIsFull(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhaseReady,
		ParentThreadID:  "thread-main",
		ReviewThreadID:  "thread-review",
		LastReviewText:  "请继续修改",
		SourceMessageID: "msg-review",
	}
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)
	other := addActiveReviewSurfaceForTest(svc, room)
	room.ActiveReservations["review:other"] = &state.FeishuRoomActiveReservationRecord{
		ReservationID:    "review:other",
		SurfaceSessionID: other.SurfaceSessionID,
		Reason:           "review_running",
	}

	events := svc.applyReviewSessionResult(surface, control.Action{MessageID: "msg-apply"})
	if noticeCode(events, "room_workspace_active") == "" {
		t.Fatalf("expected review apply admission notice, got %#v", events)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.LastReviewText != "请继续修改" {
		t.Fatalf("room admission failure must preserve review session: %#v", surface.ReviewSession)
	}
}

func TestReviewApplyReplacesItsOwnReservationWithPromptDispatch(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	delete(svc.root.Surfaces, surface.SurfaceSessionID)
	surface.SurfaceSessionID = "feishu:app-1:chat:oc_review"
	surface.GatewayID = "app-1"
	surface.ChatID = "oc_review"
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhaseReady,
		ParentThreadID:  "thread-main",
		ReviewThreadID:  "thread-review",
		ThreadCWD:       "/data/dl/droid",
		LastReviewText:  "请继续修改",
		SourceMessageID: "msg-review",
	}
	room := svc.ensureFeishuRoomContextForSurface(surface)
	room.ConcurrencyLimit = intPointer(1)
	room.ActiveReservations["review:current"] = &state.FeishuRoomActiveReservationRecord{
		ReservationID:    "review:current",
		SurfaceSessionID: surface.SurfaceSessionID,
		Reason:           "review_running",
	}

	events := svc.applyReviewSessionResult(surface, control.Action{MessageID: "msg-apply"})
	if noticeCode(events, "review_apply_requested") == "" {
		t.Fatalf("expected review apply to proceed after replacing its own reservation, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected review session to clear after apply, got %#v", surface.ReviewSession)
	}
	if !hasAgentCommand(events) {
		t.Fatalf("expected review apply prompt dispatch, got %#v", events)
	}
	if got := svc.feishuRoomActiveReservationCount(room); got != 1 {
		t.Fatalf("active reservations after review apply = %d, want 1", got)
	}
}

func TestStartReviewFromFinalCardDispatchesClaudeReviewFork(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	svc.MaterializeSurfaceResume(surface.SurfaceSessionID, surface.GatewayID, surface.ChatID, surface.ActorUserID, state.ProductModeNormal, agentproto.BackendClaude, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude

	finalBlock := render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}
	svc.RecordFinalCardMessage(surface.SurfaceSessionID, finalBlock, "msg-user-1", "om-final-1", "life-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewStart,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-final-1",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if noticeCode(events, "review_start_requested") == "" || !hasAgentCommand(events) {
		t.Fatalf("expected Claude review fork dispatch, got %#v", events)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Backend != agentproto.BackendClaude || surface.ReviewSession.ExecutorKind != state.ReviewExecutorClaudeForkSession {
		t.Fatalf("expected Claude review session, got %#v", surface.ReviewSession)
	}
	var command *agentproto.Command
	for i := range events {
		if events[i].Command != nil {
			command = events[i].Command
		}
	}
	if command == nil || command.Kind != agentproto.CommandPromptSend || command.Target.Purpose != agentproto.PromptPurposeReview || command.Target.ExecutionMode != agentproto.PromptExecutionModeForkEphemeral {
		t.Fatalf("unexpected Claude review command: %#v", command)
	}
}

func TestStartReviewCommandBuildsDetachedReviewCommandFromCurrentThread(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-1",
		Text:             "/review uncommitted",
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start target: %#v", command.Target)
	}
	if command.Review.Delivery != agentproto.ReviewDeliveryDetached || command.Review.Target.Kind != agentproto.ReviewTargetKindUncommittedChanges {
		t.Fatalf("unexpected review request: %#v", command.Review)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhasePending || surface.ReviewSession.ParentThreadID != "thread-main" || surface.ReviewSession.SourceMessageID != "msg-review-1" {
		t.Fatalf("unexpected pending review session: %#v", surface.ReviewSession)
	}
}

func TestStartReviewCommandUsesParentWhenSelectionIsReviewThread(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	svc.releaseSurfaceThreadClaim(surface)
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-review") {
		t.Fatal("expected test surface to claim review thread")
	}
	surface.SelectedThreadID = "thread-review"
	surface.ReviewSession = nil

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-1",
		Text:             "/review uncommitted",
	})

	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("expected review start to use parent thread, got %#v", command.Target)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.ParentThreadID != "thread-main" {
		t.Fatalf("expected pending review session to store parent thread, got %#v", surface.ReviewSession)
	}
}

func TestStartCommitReviewCommandUsesParentWhenSelectionIsReviewThread(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	latest := commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")
	svc.releaseSurfaceThreadClaim(surface)
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-review") {
		t.Fatal("expected test surface to claim review thread")
	}
	surface.SelectedThreadID = "thread-review"
	surface.ReviewSession = nil

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-commit",
		Text:             "/review commit " + latest.ShortSHA,
	})

	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("expected commit review start to use parent thread, got %#v", command.Target)
	}
	if command.Review.Target.Kind != agentproto.ReviewTargetKindCommit || command.Review.Target.CommitSHA != latest.SHA {
		t.Fatalf("unexpected commit review target: %#v", command.Review.Target)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.ParentThreadID != "thread-main" {
		t.Fatalf("expected pending commit review session to store parent thread, got %#v", surface.ReviewSession)
	}
}

func TestResolveFinalBlockCommitReviewTargetsSkipsGitLogWithoutCommitCandidate(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	_ = commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")
	marker := filepath.Join(t.TempDir(), "git-log-invoked")
	t.Setenv("PATH", fakeReviewSessionGitLogMarkerForTest(t, marker))

	commits := svc.ResolveFinalBlockCommitReviewTargets(surface.SurfaceSessionID, render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		Text:       "这是一条普通最终答复，没有提交记录。",
		Final:      true,
	}, "这是一条普通最终答复，没有提交记录。")

	if len(commits) != 0 {
		t.Fatalf("did not expect commit review targets, got %#v", commits)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected final block without commit candidate to skip git log, marker err=%v", err)
	}
}

func TestBareReviewCommandOpensReviewRootPage(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	_ = commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-root",
		Text:             "/review",
	})

	if len(events) != 1 || events[0].PageView == nil {
		t.Fatalf("expected one review root page event, got %#v", events)
	}
	page := events[0].PageView
	if page.Title != "审阅代码变更" {
		t.Fatalf("unexpected review root title: %#v", page)
	}
	if len(page.Sections) != 1 || len(page.Sections[0].Entries) != 2 {
		t.Fatalf("expected two review root entries, got %#v", page.Sections)
	}
	if svc.activeReviewPicker(surface) != nil {
		t.Fatalf("did not expect commit picker runtime when opening root page, got %#v", svc.activeReviewPicker(surface))
	}
	if surface.ReviewSession != nil {
		t.Fatalf("did not expect review session when opening root page, got %#v", surface.ReviewSession)
	}
}

func TestReviewRootActionStartsUncommittedReview(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	writeReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewStartUncommitted,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-root-1",
		Text:             "/review uncommitted",
		CatalogFamilyID:  control.FeishuCommandReview,
		CatalogVariantID: "review.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected review start command, got %#v", events)
	}
}

func TestReviewRootActionOpensCommitPicker(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	latest := commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewOpenCommitPicker,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-root-1",
		Text:             "/review commit",
		CatalogFamilyID:  control.FeishuCommandReview,
		CatalogVariantID: "review.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if len(events) != 1 || events[0].PageView == nil {
		t.Fatalf("expected one picker page event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "选择提交记录" {
		t.Fatalf("unexpected commit picker catalog: %#v", catalog)
	}
	form := catalog.Sections[0].Entries[0].Form
	if len(form.Field.Options) == 0 || form.Field.Options[0].Value != latest.SHA {
		t.Fatalf("expected latest commit option first, got %#v", form.Field.Options)
	}
}

func TestStartReviewCommandRejectsCleanRepo(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-1",
		Text:             "/review uncommitted",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_repo_clean" {
		t.Fatalf("expected clean-repo notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("did not expect review session for clean repo, got %#v", surface.ReviewSession)
	}
}

func TestStartReviewCommitCommandOpensPickerPage(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	latest := commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-picker",
		Text:             "/review commit",
	})

	if len(events) != 1 || events[0].PageView == nil {
		t.Fatalf("expected one picker page event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "选择提交记录" || catalog.TrackingKey == "" {
		t.Fatalf("unexpected commit picker catalog: %#v", catalog)
	}
	if len(catalog.Sections) != 1 || len(catalog.Sections[0].Entries) != 1 || catalog.Sections[0].Entries[0].Form == nil {
		t.Fatalf("expected single form entry, got %#v", catalog.Sections)
	}
	form := catalog.Sections[0].Entries[0].Form
	if form == nil || form.Field.Kind != control.CommandCatalogFormFieldSelectStatic {
		t.Fatalf("unexpected commit picker form: %#v", form)
	}
	if got := strings.TrimSpace(anyStringMapValue(form.SubmitValue, "kind")); got != "page_local_submit" {
		t.Fatalf("expected local submit payload on review commit picker, got %#v", form.SubmitValue)
	}
	if got := strings.TrimSpace(anyStringMapValue(form.SubmitValue, "action_kind")); got != string(control.ActionReviewCommand) {
		t.Fatalf("unexpected review commit submit action kind: %#v", form.SubmitValue)
	}
	if got := strings.TrimSpace(anyStringMapValue(form.SubmitValue, "action_arg_prefix")); got != "commit" {
		t.Fatalf("unexpected review commit submit action arg prefix: %#v", form.SubmitValue)
	}
	if got := strings.TrimSpace(anyStringMapValue(form.SubmitValue, "field_name")); got != reviewCommitPickerFieldName {
		t.Fatalf("unexpected review commit submit field name: %#v", form.SubmitValue)
	}
	if len(form.Field.Options) == 0 || form.Field.Options[0].Value != latest.SHA {
		t.Fatalf("expected latest commit option first, got %#v", form.Field.Options)
	}
	if len(catalog.RelatedButtons) != 1 {
		t.Fatalf("expected one cancel button, got %#v", catalog.RelatedButtons)
	}
	cancelValue := catalog.RelatedButtons[0].CallbackValue
	if got := strings.TrimSpace(anyStringMapValue(cancelValue, "kind")); got != "page_local_action" {
		t.Fatalf("expected local cancel payload, got %#v", cancelValue)
	}
	if got := strings.TrimSpace(anyStringMapValue(cancelValue, "action_kind")); got != string(control.ActionReviewCommand) {
		t.Fatalf("unexpected review cancel action kind: %#v", cancelValue)
	}
	if got := strings.TrimSpace(anyStringMapValue(cancelValue, "action_arg")); got != "cancel" {
		t.Fatalf("unexpected review cancel action arg: %#v", cancelValue)
	}
	if flow := svc.activeOwnerCardFlow(surface); flow == nil || flow.Kind != ownerCardFlowKindReviewPicker {
		t.Fatalf("expected active review picker flow, got %#v", flow)
	}
	if record := svc.activeReviewPicker(surface); record == nil || record.InstanceID != "inst-1" || record.ParentThreadID != "thread-main" {
		t.Fatalf("expected stored review picker context, got %#v", record)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("did not expect review session before picker submit, got %#v", surface.ReviewSession)
	}
}

func TestStartReviewCommitCommandBuildsDetachedReviewCommand(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	latest := commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-commit",
		Text:             "/review commit " + latest.ShortSHA,
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start target: %#v", command.Target)
	}
	if command.Review.Target.Kind != agentproto.ReviewTargetKindCommit || command.Review.Target.CommitSHA != latest.SHA || command.Review.Target.CommitTitle != latest.Subject {
		t.Fatalf("unexpected commit review target: %#v", command.Review.Target)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.TargetLabel != "提交 "+latest.ShortSHA || surface.ReviewSession.SourceMessageID != "msg-review-commit" {
		t.Fatalf("unexpected pending commit review session: %#v", surface.ReviewSession)
	}
}

func TestStartReviewCommitFromPickerUsesStoredContext(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	latest := commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")

	pickerEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-picker",
		Text:             "/review commit",
	})
	if len(pickerEvents) != 1 || pickerEvents[0].PageView == nil {
		t.Fatalf("expected picker page, got %#v", pickerEvents)
	}
	svc.RecordPageTrackingMessage(surface.SurfaceSessionID, pickerEvents[0].PageView.TrackingKey, "om-review-picker-1")

	surface.AttachedInstanceID = "inst-1"
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-picker-1",
		ActorUserID:      "user-1",
		Text:             "/review commit " + latest.SHA,
		CatalogFamilyID:  control.FeishuCommandReview,
		CatalogVariantID: "review.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-picker",
		},
	})

	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected picker submit to start review, got %#v", events)
	}
	if events[1].Command.Review.Target.Kind != agentproto.ReviewTargetKindCommit || events[1].Command.Review.Target.CommitSHA != latest.SHA {
		t.Fatalf("unexpected picker review target: %#v", events[1].Command.Review.Target)
	}
	if svc.activeReviewPicker(surface) != nil {
		t.Fatalf("expected review picker runtime to clear after submit, got %#v", svc.activeReviewPicker(surface))
	}
}

func TestStartReviewCommitFromPickerRejectsInstanceSwitch(t *testing.T) {
	svc, surface, repoRoot := newReviewSessionService(t)
	_ = commitReviewSessionRepoFile(t, repoRoot, "docs/guide.md", "guide\n", "review target commit")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "other",
		WorkspaceRoot: repoRoot,
		WorkspaceKey:  repoRoot,
		ShortName:     "other",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	pickerEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-picker",
		Text:             "/review commit",
	})
	if len(pickerEvents) != 1 || pickerEvents[0].PageView == nil {
		t.Fatalf("expected picker page, got %#v", pickerEvents)
	}
	svc.RecordPageTrackingMessage(surface.SurfaceSessionID, pickerEvents[0].PageView.TrackingKey, "om-review-picker-1")
	surface.AttachedInstanceID = "inst-2"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-picker-1",
		ActorUserID:      "user-1",
		Text:             "/review commit deadbeef",
		CatalogFamilyID:  control.FeishuCommandReview,
		CatalogVariantID: "review.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-picker",
		},
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_source_instance_changed" {
		t.Fatalf("expected instance-changed notice, got %#v", events)
	}
	if svc.activeReviewPicker(surface) != nil {
		t.Fatalf("expected stale picker runtime to clear, got %#v", svc.activeReviewPicker(surface))
	}
}

func TestApplyReviewSessionResultBuildsParentPromptAndClearsSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "建议先补一条 review 回归测试。",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	if len(events) < 2 {
		t.Fatalf("expected review apply to enqueue and dispatch work, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "review_apply_requested" {
		t.Fatalf("expected review apply notice first, got %#v", events)
	}
	var command *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = event.Command
			break
		}
	}
	if command == nil {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	if command.Target.ThreadID != "thread-main" || command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting || command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected apply-review target: %#v", command.Target)
	}
	if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != reviewApplyPromptPrefix+"建议先补一条 review 回归测试。" {
		t.Fatalf("unexpected apply-review prompt: %#v", command.Prompt)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected review session to clear after apply, got %#v", surface.ReviewSession)
	}
}

func TestApplyReviewSessionResultRequiresReadyPhase(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "尚未固化的审阅文本",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-active",
	})

	if noticeCode(events, "review_result_not_ready") == "" {
		t.Fatalf("expected active review session to reject apply, got %#v", events)
	}
	if hasAgentCommand(events) {
		t.Fatalf("active review session must not dispatch parent prompt, got %#v", events)
	}
	if surface.ReviewSession == nil {
		t.Fatal("active review session must remain available after rejected apply")
	}
}

func TestApplyReviewSessionResultRecoversReviewThreadSelection(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "建议把审阅意见带回主会话。",
	}
	svc.releaseSurfaceThreadClaim(surface)
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-review") {
		t.Fatal("expected test surface to claim review thread")
	}
	surface.SelectedThreadID = "thread-review"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	var command *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = event.Command
			break
		}
	}
	if command == nil {
		t.Fatalf("expected apply from recovered review selection to enqueue prompt, got %#v", events)
	}
	if command.Target.ThreadID != "thread-main" || command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected recovered apply target: %#v", command.Target)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected recovered apply to restore parent selection, got %q", surface.SelectedThreadID)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected recovered apply to clear review session, got %#v", surface.ReviewSession)
	}
}

func TestDiscardReviewSessionClearsRuntime(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_discarded" {
		t.Fatalf("expected discard notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected review session to clear, got %#v", surface.ReviewSession)
	}
}

func TestDiscardActiveReviewInterruptsBeforeClearingSession(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	if len(events) != 2 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnInterrupt || noticeCode(events, "review_discard_requested") == "" {
		t.Fatalf("active discard must request interrupt before exit, got %#v", events)
	}
	if events[0].Command.Target.ThreadID != "thread-review" || events[0].Command.Target.TurnID != "turn-review-1" {
		t.Fatalf("active discard interrupt target = %#v", events[0].Command.Target)
	}
	if surface.ReviewSession == nil || !surface.ReviewSession.ExitRequested {
		t.Fatalf("active discard must keep session until terminal turn, got %#v", surface.ReviewSession)
	}
	repeated := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	if len(repeated) != 1 || repeated[0].Command != nil || noticeCode(repeated, "review_discard_requested") == "" {
		t.Fatalf("repeated active discard must not send another interrupt, got %#v", repeated)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "interrupted",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	if surface.ReviewSession != nil {
		t.Fatalf("terminal interrupted turn must finish requested discard, got %#v", surface.ReviewSession)
	}
}

func TestDiscardDispatchingReviewWaitsForInterruptibleTurn(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhasePending,
		ParentThreadID: "thread-main",
	}
	surface.ActiveQueueItemID = "queue-review"
	surface.QueueItems["queue-review"] = &state.QueueItemRecord{
		ID:     "queue-review",
		Status: state.QueueItemDispatching,
		FrozenDispatchPlan: agentproto.PromptDispatchPlan{
			ExecutionMode:  agentproto.PromptExecutionModeForkEphemeral,
			SourceThreadID: "thread-main",
			Purpose:        agentproto.PromptPurposeReview,
		},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	if noticeCode(events, "review_result_not_ready") == "" || surface.ReviewSession == nil {
		t.Fatalf("pre-start discard must wait rather than orphan dispatch, events=%#v session=%#v", events, surface.ReviewSession)
	}
}

func TestDiscardReviewSessionRecoversReviewThreadSelection(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
	}
	svc.releaseSurfaceThreadClaim(surface)
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-review") {
		t.Fatal("expected test surface to claim review thread")
	}
	surface.SelectedThreadID = "thread-review"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_discarded" {
		t.Fatalf("expected discard notice after recovering review selection, got %#v", events)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected recovered discard to restore parent selection, got %q", surface.SelectedThreadID)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected recovered discard to clear review session, got %#v", surface.ReviewSession)
	}
}

func anyStringMapValue(value map[string]any, key string) string {
	if len(value) == 0 {
		return ""
	}
	current, _ := value[key].(string)
	return current
}
