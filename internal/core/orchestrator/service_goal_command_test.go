package orchestrator

import (
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
		if event.PageView != nil && event.PageView.Phase == "failed" && containsSectionLine(event.PageView, "无法操作 Goal") {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("expected error page for missing thread, got %#v", events)
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
