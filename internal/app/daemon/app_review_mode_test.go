package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newReviewModeAppForTest(t *testing.T) (*App, *messageIDAssigningGateway, string) {
	t.Helper()
	repoRoot := initReviewModeRepo(t)
	gateway := &messageIDAssigningGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       7,
		StartedAt: time.Date(2026, 4, 27, 8, 0, 0, 0, time.UTC),
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: repoRoot,
		WorkspaceKey:  repoRoot,
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {
				ThreadID: "thread-main",
				CWD:      repoRoot,
				Loaded:   true,
			},
			"thread-review": {
				ThreadID: "thread-review",
				CWD:      repoRoot,
				Loaded:   true,
				Source: &agentproto.ThreadSourceRecord{
					Kind:           agentproto.ThreadSourceKindReview,
					ParentThreadID: "thread-main",
				},
			},
		},
	})
	materializeAttachedSurfaceForFinalCardTest(app, "surface-1", "app-1", "chat-1", "user-1", "inst-1", repoRoot)
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-main",
	})
	return app, gateway, repoRoot
}

func initReviewModeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	runReviewModeGit(t, root, "init")
	runReviewModeGit(t, root, "config", "user.email", "review-mode-tests@example.com")
	runReviewModeGit(t, root, "config", "user.name", "Review Mode Tests")
	return root
}

func runReviewModeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func writeReviewModeRepoFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", fullPath, err)
	}
}

func commitReviewModeRepoFile(t *testing.T, root, relativePath, content, message string) string {
	t.Helper()
	writeReviewModeRepoFile(t, root, relativePath, content)
	runReviewModeGit(t, root, "add", relativePath)
	runReviewModeGit(t, root, "-c", "user.name=review-mode-tests", "-c", "user.email=review-mode-tests@example.com", "commit", "-q", "-m", message)
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse --short HEAD failed: %v\n%s", err, string(output))
	}
	return strings.TrimSpace(string(output))
}

func TestDeliverUIEventDoesNotAddReviewEntryButtonToNormalFinalCardWithoutFileChanges(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver final block: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复" {
		t.Fatalf("unexpected normal final title: %#v", ops[0])
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button without file changes, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventAddsReviewEntryButtonWhenRepoHasUncommittedChanges(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	writeReviewModeRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver final block with dirty repo: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if !operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("expected review entry button with dirty repo, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventDoesNotAddReviewEntryButtonWhenRepoIsCleanEvenIfFinalCardIncludesFileChanges(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
		FileChangeSummary: &control.FileChangeSummary{
			FileCount:    1,
			AddedLines:   1,
			RemovedLines: 0,
			Files: []control.FileChangeSummaryEntry{
				{Path: "docs/guide.md", AddedLines: 1},
			},
		},
	})
	if err != nil {
		t.Fatalf("deliver final block with clean repo and file summary: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button for clean repo, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventAddsReviewEntryButtonForRepoWideDirtyStateFromSubdir(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	threadCWD := filepath.Join(repoRoot, "frontend")
	if err := os.MkdirAll(threadCWD, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", threadCWD, err)
	}
	app.service.Instance("inst-1").Threads["thread-main"].CWD = threadCWD
	writeReviewModeRepoFile(t, repoRoot, "backend/todo.txt", "pending backend change\n")

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "已经处理完成。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver final block with repo-wide dirty state: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if !operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("expected review entry button for repo-wide dirty state, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventMarksReviewFinalCardAndAddsExitButtons(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)
	app.service.Surfaces()[0].ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		TargetLabel:    "提交 abc1234",
	}

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-final-main-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-review",
			TurnID:     "turn-review-1",
			ItemID:     "item-review-1",
			Text:       "建议先补一条测试。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver review final block: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one review final card, got %#v", ops)
	}
	if !strings.HasPrefix(ops[0].CardTitle, reviewCardTitlePrefix) {
		t.Fatalf("expected review title prefix, got %#v", ops[0])
	}
	if !strings.Contains(ops[0].CardTitle, "提交 abc1234") {
		t.Fatalf("expected review title to include commit target label, got %#v", ops[0].CardTitle)
	}
	if !operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewDiscard)) {
		t.Fatalf("expected discard button, got %#v", ops[0].CardElements)
	}
	if !operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewApply)) {
		t.Fatalf("expected apply button, got %#v", ops[0].CardElements)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button on review final card, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventKeepsReviewFinalCardSuppressedAfterSessionRuntimeClears(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-final-main-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-review",
			TurnID:     "turn-review-2",
			ItemID:     "item-review-2",
			Text:       "建议把验证逻辑抽到 helper。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver detached review final block without session runtime: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one review final card, got %#v", ops)
	}
	if !strings.HasPrefix(ops[0].CardTitle, reviewCardTitlePrefix) {
		t.Fatalf("expected review title prefix even after session runtime clears, got %#v", ops[0].CardTitle)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewStart)) {
		t.Fatalf("did not expect review entry button on detached review final card, got %#v", ops[0].CardElements)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewCommand)) {
		t.Fatalf("did not expect commit review buttons on detached review final card, got %#v", ops[0].CardElements)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewDiscard)) {
		t.Fatalf("did not expect exit buttons once session runtime is gone, got %#v", ops[0].CardElements)
	}
	if operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewApply)) {
		t.Fatalf("did not expect exit buttons once session runtime is gone, got %#v", ops[0].CardElements)
	}
}

func TestDeliverUIEventAddsCommitReviewButtonWhenFinalBodyMentionsRecentCommit(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	shortSHA := commitReviewModeRepoFile(t, repoRoot, "docs/guide.md", "committed change\n", "review target commit")

	err := app.deliverUIEventWithContext(context.Background(), eventcontract.Event{
		Kind:             eventcontract.KindBlockCommitted,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "msg-1",
		Block: &render.Block{
			Kind:       render.BlockAssistantMarkdown,
			InstanceID: "inst-1",
			ThreadID:   "thread-main",
			TurnID:     "turn-main-1",
			ItemID:     "item-main-1",
			Text:       "这轮已经生成 commit `" + shortSHA + "`。",
			Final:      true,
		},
	})
	if err != nil {
		t.Fatalf("deliver final block with commit mention: %v", err)
	}

	ops := gateway.snapshotOperations()
	if len(ops) != 1 {
		t.Fatalf("expected one final card, got %#v", ops)
	}
	if !operationHasActionValue(ops[0], "page_local_action", "action_kind", string(control.ActionReviewCommand)) {
		t.Fatalf("expected commit review button, got %#v", ops[0].CardElements)
	}
}

func TestHandleGatewayActionStartsDetachedReviewFromFinalCard(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	writeReviewModeRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	app.service.RecordFinalCardMessage("surface-1", render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}, "msg-1", "om-final-1", app.daemonLifecycleID)

	result := handleGatewayActionForTest(context.Background(), app, control.Action{
		Kind:             control.ActionReviewStart,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result != nil {
		t.Fatalf("expected review start to keep source final card and stay append-only, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "正在进入审阅" {
		t.Fatalf("expected one appended entering-review notice card, got %#v", ops)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandReviewStart || sent[0].Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start command: %#v", sent)
	}
}

func TestHandleGatewayActionStartsDetachedReviewFromCurrentThreadCommand(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	writeReviewModeRepoFile(t, repoRoot, "docs/guide.md", "pending review change\n")
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	result := handleGatewayActionForTest(context.Background(), app, control.Action{
		Kind:             control.ActionReviewCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-review-1",
		Text:             "/review uncommitted",
	})

	if result != nil {
		t.Fatalf("expected command review start to stay append-only, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "正在进入审阅" {
		t.Fatalf("expected one appended entering-review notice card, got %#v", ops)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandReviewStart || sent[0].Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start command: %#v", sent)
	}
}

func TestHandleGatewayActionShowsReviewRootPageInPlace(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	commitReviewModeRepoFile(t, repoRoot, "docs/guide.md", "committed change\n", "review target commit")
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	result := handleGatewayActionForTest(context.Background(), app, control.Action{
		Kind:             control.ActionReviewCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-review-append-1",
		Text:             "/review",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected bare review to replace current card with root page, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "审阅代码变更" {
		t.Fatalf("expected review root page title, got %#v", result.ReplaceCurrentCard)
	}
	if len(sent) != 0 {
		t.Fatalf("did not expect review start command for bare review root open, got %#v", sent)
	}
	if len(gateway.snapshotOperations()) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.snapshotOperations())
	}
}

func TestHandleGatewayActionStartsDetachedCommitReviewFromFinalCardLocalCallback(t *testing.T) {
	app, gateway, repoRoot := newReviewModeAppForTest(t)
	shortSHA := commitReviewModeRepoFile(t, repoRoot, "docs/guide.md", "committed change\n", "review target commit")
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	app.service.RecordFinalCardMessage("surface-1", render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}, "msg-1", "om-final-1", app.daemonLifecycleID)

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionReviewCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-1",
		Text:             "/review commit " + shortSHA,
		LocalPageAction:  true,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result != nil {
		t.Fatalf("expected commit review start to keep source final card and stay append-only, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "正在进入审阅" {
		t.Fatalf("expected one appended entering-review notice card, got %#v", ops)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandReviewStart {
		t.Fatalf("unexpected review start command: %#v", sent)
	}
	if sent[0].Review.Target.Kind != agentproto.ReviewTargetKindCommit || !strings.HasPrefix(sent[0].Review.Target.CommitSHA, shortSHA) {
		t.Fatalf("unexpected commit review target: %#v", sent[0].Review.Target)
	}
}

func TestHandleGatewayActionRejectsExpiredCommitReviewFinalCardLocalCallback(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		t.Fatalf("did not expect command dispatch for expired commit review card: %#v", command)
		return nil
	}

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionReviewCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-old-commit-1",
		Text:             "/review commit abc1234",
		LocalPageAction:  true,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "old-daemon",
		},
	})

	if result != nil {
		t.Fatalf("expected expired commit review card to bypass inline result, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "旧卡片已过期" {
		t.Fatalf("expected old-card rejection notice, got %#v", ops)
	}
}

func TestHandleGatewayActionAppliesReviewResultBackToParentThread(t *testing.T) {
	app, _, _ := newReviewModeAppForTest(t)
	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}
	surface := app.service.Surfaces()[0]
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "建议先补一条 review 回归测试。",
	}

	result := handleGatewayActionForTest(context.Background(), app, control.Action{
		Kind:             control.ActionReviewApply,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-review-final-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil || result.ReplaceCurrentCard.CardTitle != "正在继续修改" {
		t.Fatalf("expected inline continue-review card, got %#v", result)
	}
	if len(sent) != 1 || sent[0].Kind != agentproto.CommandPromptSend {
		t.Fatalf("unexpected apply-review command: %#v", sent)
	}
	if sent[0].Target.ThreadID != "thread-main" || sent[0].Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting {
		t.Fatalf("unexpected apply-review target: %#v", sent[0].Target)
	}
	if len(sent[0].Prompt.Inputs) != 1 || sent[0].Prompt.Inputs[0].Text != "请根据以下审阅意见继续修改：\n\n建议先补一条 review 回归测试。" {
		t.Fatalf("unexpected apply-review prompt: %#v", sent[0].Prompt)
	}
	if app.service.ReviewSession("surface-1") != nil {
		t.Fatalf("expected review session to clear after continue")
	}
}

func TestHandleGatewayActionRejectsExpiredReviewCard(t *testing.T) {
	app, gateway, _ := newReviewModeAppForTest(t)
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		t.Fatalf("did not expect command dispatch for expired review card: %#v", command)
		return nil
	}

	result := handleGatewayActionForTest(context.Background(), app, control.Action{
		Kind:             control.ActionReviewStart,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-final-old-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "old-daemon",
		},
	})

	if result != nil {
		t.Fatalf("expected expired review card to bypass inline result, got %#v", result)
	}
	ops := gateway.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].CardTitle != "旧卡片已过期" {
		t.Fatalf("expected old-card rejection notice, got %#v", ops)
	}
}
