package orchestrator

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestGoalCommandBareIssuesGetAndRendersStatusPage(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-1",
		Text:             "/goal",
	})
	get := findAgentCommand(events, agentproto.CommandThreadGoalGet)
	if get == nil || get.Goal.Purpose != "user_control" {
		t.Fatalf("expected user_control goal get command, got %#v", events)
	}

	resultEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: get.CommandID,
		ThreadID:  "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:   "thread-1",
			Objective:  "ship it",
			Status:     "active",
			TokensUsed: 12,
		},
	})
	page := findPageEvent(resultEvents)
	if page == nil || page.Title != "Goal" {
		t.Fatalf("expected goal status page, got %#v", resultEvents)
	}
	if !containsSectionLine(page, "ship it") {
		t.Fatalf("expected objective in status page, got %#v", page.BodySections)
	}
	thread := svc.root.Instances[instanceID].Threads["thread-1"]
	if thread.ThreadGoal == nil || thread.ThreadGoal.Status != "active" {
		t.Fatalf("expected user get result to write back thread goal, got %#v", thread.ThreadGoal)
	}
}

func TestGoalCommandNewSendsSetWithObjectiveAndBudget(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-2",
		Text:             "/goal new --budget 1200 完成登录流程重构",
	})
	set := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if set == nil || set.Goal.Objective != "完成登录流程重构" || set.Goal.Purpose != "user_control" {
		t.Fatalf("unexpected goal set command: %#v", set)
	}
	if set.Goal.TokenBudget == nil || *set.Goal.TokenBudget != 1200 {
		t.Fatalf("expected budget 1200, got %#v", set.Goal.TokenBudget)
	}
}

func TestGoalCommandNewWithoutObjectiveOpensOwnerForm(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-form-1",
		Text:             "/goal new",
	})
	if findAgentCommand(events, agentproto.CommandThreadGoalSet) != nil {
		t.Fatalf("goal new without objective must open form, got %#v", events)
	}
	page := findPageEvent(events)
	if page == nil || page.Title != "创建 Goal" {
		t.Fatalf("expected create goal form page, got %#v", events)
	}
	form := findCommandForm(page)
	if form == nil || form.CommandText != "/goal new" || form.Field.Name != "command_args" {
		t.Fatalf("expected reusable command_args form on /goal new, got %#v", page.Sections)
	}
	if form.Field.Kind != control.CommandCatalogFormFieldText {
		t.Fatalf("expected text objective field, got %#v", form.Field)
	}

	submitted := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-form-2",
		Text:             "/goal new 完成登录流程重构 --budget 800",
	})
	set := findAgentCommand(submitted, agentproto.CommandThreadGoalSet)
	if set == nil || set.Goal.Objective != "完成登录流程重构" || set.Goal.Purpose != "user_control" {
		t.Fatalf("expected form submit to issue goal set, got %#v", submitted)
	}
	if set.Goal.TokenBudget == nil || *set.Goal.TokenBudget != 800 {
		t.Fatalf("expected form budget 800, got %#v", set.Goal.TokenBudget)
	}
}

func TestGoalCommandEditWithoutObjectivePrefillsCurrentGoal(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	inst := svc.root.Instances["inst-1"]
	inst.Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{
		ThreadID:    "thread-1",
		Objective:   "当前目标",
		Status:      "active",
		TokenBudget: ptrInt64(500),
	}
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-edit-1",
		Text:             "/goal edit",
	})
	page := findPageEvent(events)
	if page == nil || page.Title != "编辑 Goal" {
		t.Fatalf("expected edit goal form page, got %#v", events)
	}
	form := findCommandForm(page)
	if form == nil || form.CommandText != "/goal edit" {
		t.Fatalf("expected reusable command_args form on /goal edit, got %#v", page.Sections)
	}
	if got := strings.TrimSpace(form.Field.DefaultValue); got != "当前目标 --budget 500" {
		t.Fatalf("expected prefilled objective and budget, got %q", got)
	}
}

func TestGoalCommandCompleteStatusPageOffersNewGoalEntry(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-complete-1",
		Text:             "/goal",
	})
	get := findAgentCommand(events, agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatalf("expected goal get command, got %#v", events)
	}
	resultEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: get.CommandID,
		ThreadID:  "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:        "thread-1",
			Objective:       "已完成目标",
			Status:          "complete",
			TokensUsed:      100,
			TimeUsedSeconds: 42,
			TokenBudget:     ptrInt64(200),
		},
	})
	page := findPageEvent(resultEvents)
	if page == nil {
		t.Fatalf("expected goal status page, got %#v", resultEvents)
	}
	var sawNew bool
	for _, button := range page.RelatedButtons {
		if button.Label == "新建" && button.CommandText == "/goal new" {
			sawNew = true
		}
	}
	if !sawNew {
		t.Fatalf("expected complete status page to offer new goal entry, got %#v", page.RelatedButtons)
	}
}

func TestGoalCommandClearRequiresConfirm(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-3",
		Text:             "/goal clear",
	})
	if findAgentCommand(confirmEvents, agentproto.CommandThreadGoalClear) != nil {
		t.Fatalf("clear must require confirmation, got %#v", confirmEvents)
	}
	page := findPageEvent(confirmEvents)
	if page == nil || page.Title != "清除 Goal" {
		t.Fatalf("expected clear confirmation page, got %#v", confirmEvents)
	}

	confirmed := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-4",
		Text:             "/goal clear --confirm",
	})
	if findAgentCommand(confirmed, agentproto.CommandThreadGoalClear) == nil {
		t.Fatalf("expected clear command after confirmation, got %#v", confirmed)
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}

func findCommandForm(page *control.FeishuPageView) *control.CommandCatalogForm {
	if page == nil {
		return nil
	}
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			if entry.Form != nil {
				return entry.Form
			}
		}
	}
	return nil
}

func TestGoalCommandRejectsNonCodexOrMissingThread(t *testing.T) {
	now := nowForTest()
	svc := newServiceForTest(&now)
	svc.UpsertInstance(threadLifecycleInstance())
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-5",
		Text:             "/goal",
	})
	if findAgentCommand(events, agentproto.CommandThreadGoalGet) != nil {
		t.Fatalf("goal without selected thread must fail closed, got %#v", events)
	}
	foundError := false
	for _, event := range events {
		if event.PageView != nil && !event.PageView.Sealed && containsSectionLine(event.PageView, "无法操作 Goal") {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("expected error page for missing thread, got %#v", events)
	}
}

func TestGoalCommandClearFingerprintDriftFailsClosed(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-stale-1",
		Text:             "/goal clear",
	})
	inst := svc.root.Instances["inst-1"]
	inst.Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{
		ThreadID:  "thread-1",
		Objective: "另一个客户端改了目标",
		Status:    "active",
	}

	confirmed := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-stale-2",
		Text:             "/goal clear --confirm",
	})
	if findAgentCommand(confirmed, agentproto.CommandThreadGoalClear) != nil {
		t.Fatalf("stale clear confirmation must fail closed, got %#v", confirmed)
	}
	page := findPageEvent(confirmed)
	if page == nil || page.Title != "Goal 已变化" {
		t.Fatalf("expected stale goal page, got %#v", confirmed)
	}
	if !strings.Contains(page.StatusText, "已取消操作") {
		t.Fatalf("expected stale guidance in page, got %#v", page)
	}
	if len(svc.pendingGoalFingerprints) != 0 {
		t.Fatalf("expected stale fingerprint to be consumed, got %#v", svc.pendingGoalFingerprints)
	}
}

func TestGoalCommandEditFingerprintDriftFailsClosed(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-edit-stale-1",
		Text:             "/goal edit",
	})
	inst := svc.root.Instances["inst-1"]
	inst.Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{
		ThreadID:  "thread-1",
		Objective: "被外部改过",
		Status:    "paused",
	}

	submitted := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-edit-stale-2",
		Text:             "/goal edit 新目标",
	})
	if findAgentCommand(submitted, agentproto.CommandThreadGoalSet) != nil {
		t.Fatalf("stale edit submit must fail closed, got %#v", submitted)
	}
	page := findPageEvent(submitted)
	if page == nil || page.Title != "Goal 已变化" {
		t.Fatalf("expected stale goal page, got %#v", submitted)
	}
}

func TestGoalCommandDispatchFailureCleansUpAndShowsError(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-fail-1",
		Text:             "/goal pause",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil || pause.Goal.Status != "paused" {
		t.Fatalf("expected goal pause command, got %#v", events)
	}
	if _, ok := svc.goalUserCommands[pause.CommandID]; !ok {
		t.Fatalf("expected pending user command, got %#v", svc.goalUserCommands)
	}

	failed := svc.HandleCommandDispatchFailure(surfaceID, pause.CommandID, fmt.Errorf("relay send failed"))
	page := findPageEvent(failed)
	if page == nil || page.Title != "Goal 操作失败" {
		t.Fatalf("expected dispatch failure error page, got %#v", failed)
	}
	if !strings.Contains(page.StatusText, "relay send failed") {
		t.Fatalf("expected failure reason on page, got %#v", page)
	}
	if len(svc.goalUserCommands) != 0 {
		t.Fatalf("expected failed user command to be cleaned up, got %#v", svc.goalUserCommands)
	}
}

func TestGoalCommandErrorPageDoesNotClaimNoGoal(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-err-1",
		Text:             "/goal pause",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatalf("expected pause command, got %#v", events)
	}
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:         agentproto.EventThreadGoalCommandResult,
		CommandID:    pause.CommandID,
		ThreadID:     "thread-1",
		ErrorMessage: "Codex 拒绝了暂停",
	})
	page := findPageEvent(result)
	if page == nil || page.StatusKind != "error" {
		t.Fatalf("expected error page, got %#v", result)
	}
	if containsSectionLine(page, "当前会话没有 Goal") {
		t.Fatalf("error page must not claim no goal, got %#v", page)
	}
	if !strings.Contains(page.StatusText, "Codex 拒绝了暂停") {
		t.Fatalf("expected failure reason on page, got %#v", page)
	}
	hasRefresh := false
	for _, button := range page.RelatedButtons {
		if button.Label == "刷新" && button.CommandText == "/goal" {
			hasRefresh = true
		}
	}
	if !hasRefresh {
		t.Fatalf("expected refresh button on error page, got %#v", page.RelatedButtons)
	}
}

func TestGoalCommandClearSuccessUsesInfoStyle(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	confirm := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-cleared-1",
		Text:             "/goal clear",
	})
	if findAgentCommand(confirm, agentproto.CommandThreadGoalClear) != nil {
		t.Fatalf("clear must require confirm, got %#v", confirm)
	}
	confirmed := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-cleared-2",
		Text:             "/goal clear --confirm",
	})
	clear := findAgentCommand(confirmed, agentproto.CommandThreadGoalClear)
	if clear == nil {
		t.Fatalf("expected clear command, got %#v", confirmed)
	}
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:        agentproto.EventThreadGoalCommandResult,
		CommandID:   clear.CommandID,
		ThreadID:    "thread-1",
		GoalCleared: true,
	})
	page := findPageEvent(result)
	if page == nil || page.Title != "Goal 已清除" {
		t.Fatalf("expected cleared page, got %#v", result)
	}
	if page.StatusKind == "error" {
		t.Fatalf("clear success must not use error style, got %#v", page)
	}
	if !containsSectionLine(page, "当前会话没有 Goal") {
		t.Fatalf("expected cleared page to show creation entry, got %#v", page)
	}
}

func TestGoalCommandSetResultWithoutSnapshotShowsSyncPending(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-sync-1",
		Text:             "/goal pause",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatalf("expected pause command, got %#v", events)
	}
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: pause.CommandID,
		ThreadID:  "thread-1",
	})
	page := findPageEvent(result)
	if page == nil {
		t.Fatalf("expected result page, got %#v", result)
	}
	if containsSectionLine(page, "当前会话没有 Goal") {
		t.Fatalf("set success without snapshot must not claim no goal, got %#v", page)
	}
	if !strings.Contains(page.StatusText, "正在同步") {
		t.Fatalf("expected sync pending page, got %#v", page)
	}
}

func TestGoalCommandMalformedBudgetFailsClosed(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	for _, text := range []string{"/goal new 完成登录 --budget abc", "/goal new 完成登录 --budget=abc"} {
		events := svc.ApplySurfaceAction(control.Action{
			Kind:             control.ActionGoalCommand,
			SurfaceSessionID: surfaceID,
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			MessageID:        "om-goal-budget-" + text,
			Text:             text,
		})
		if findAgentCommand(events, agentproto.CommandThreadGoalSet) != nil {
			t.Fatalf("malformed budget must fail closed, got %#v", events)
		}
		foundError := false
		for _, event := range events {
			if event.PageView != nil && containsSectionLine(event.PageView, "无法解析 --budget") {
				foundError = true
			}
		}
		if !foundError {
			t.Fatalf("expected budget parse error page for %q, got %#v", text, events)
		}
	}
}

func TestGoalCommandCardFlowPatchesLoadingAndResultOnSameMessage(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionGoalCommand,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-goal-card-1",
		Text:             "/goal pause",
		CatalogFamilyID:  control.FeishuCommandGoal,
		CatalogVariantID: "goal.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "lifecycle-1",
		},
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatalf("expected pause command, got %#v", events)
	}
	loading := findPageEvent(events)
	if loading == nil || loading.MessageID != "om-goal-card-1" || !loading.Patchable {
		t.Fatalf("expected patchable loading card on source message, got %#v", loading)
	}
	result := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: pause.CommandID,
		ThreadID:  "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:  "thread-1",
			Objective: "ship it",
			Status:    "paused",
		},
	})
	page := findPageEvent(result)
	if page == nil || page.MessageID != "om-goal-card-1" || !page.Patchable {
		t.Fatalf("expected result to patch same message, got %#v", page)
	}
}

func findPageEvent(events []eventcontract.Event) *control.FeishuPageView {
	for _, event := range events {
		if event.PageView != nil {
			return event.PageView
		}
	}
	return nil
}

func containsSectionLine(page *control.FeishuPageView, want string) bool {
	if page == nil {
		return false
	}
	all := append(append(append([]control.FeishuCardTextSection{}, page.SummarySections...), page.BodySections...), page.NoticeSections...)
	for _, section := range all {
		for _, line := range section.Lines {
			if strings.Contains(line, want) {
				return true
			}
		}
	}
	return false
}

func nowForTest() time.Time {
	return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
}
