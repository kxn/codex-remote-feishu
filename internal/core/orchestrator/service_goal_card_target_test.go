package orchestrator

import (
	"fmt"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestGoalPagesRenderScopedControls(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	surface := svc.root.Surfaces[surfaceID]
	for _, status := range []string{"active", "paused", "complete", "blocked", "missing", "new", "edit", "clear", "error", "sync"} {
		t.Run(status, func(t *testing.T) {
			goal := &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "目标", Status: status}
			page := svc.goalStatusPage(surface, goal)
			switch status {
			case "missing":
				page = svc.goalStatusPage(surface, nil)
			case "new", "edit":
				page = svc.goalCommandFormPage(surface, status)
			case "clear":
				page = svc.goalClearConfirmPage(surface)
			case "error":
				page = svc.goalErrorPage(surface, "失败", "请刷新")
			case "sync":
				page = svc.goalSyncPendingPage(surface)
			}
			elements := projector.PageElementsWithOptions(page, "lifecycle-1", projector.PageRenderOptions{})
			var payloads []map[string]any
			walkGoalCardPayloads(elements, &payloads)
			if len(payloads) == 0 {
				t.Fatal("Goal page rendered no command controls")
			}
			if status == "active" || status == "paused" || status == "complete" {
				want := map[string]string{"active": "pause", "paused": "resume", "complete": "new"}[status]
				found := false
				for _, payload := range payloads {
					found = found || payload[frontstagecontract.CardActionPayloadKeyActionArg] == want
				}
				if !found {
					t.Fatalf("Goal %s page did not render %s control", status, want)
				}
			}
			for _, payload := range payloads {
				if payload[frontstagecontract.CardActionPayloadKeyThreadID] != "thread-1" || payload[frontstagecontract.CardActionPayloadKeyInstanceID] != instanceID {
					t.Fatalf("Goal control lost its original target: %#v", payload)
				}
				if payload[frontstagecontract.CardActionPayloadKeyCatalogFamilyID] != control.FeishuCommandGoal || payload[frontstagecontract.CardActionPayloadKeyCatalogVariantID] == "" {
					t.Fatalf("Goal control lost catalog provenance: %#v", payload)
				}
			}
		})
	}
}

func walkGoalCardPayloads(value any, payloads *[]map[string]any) {
	switch value := value.(type) {
	case []map[string]any:
		for _, item := range value {
			walkGoalCardPayloads(item, payloads)
		}
	case []any:
		for _, item := range value {
			walkGoalCardPayloads(item, payloads)
		}
	case map[string]any:
		if value[frontstagecontract.CardActionPayloadKeyActionKind] == string(control.ActionGoalCommand) {
			*payloads = append(*payloads, value)
			return
		}
		for _, item := range value {
			walkGoalCardPayloads(item, payloads)
		}
	}
}

func goalCardTestAction(surfaceID, instanceID, threadID, text string) control.Action {
	return control.Action{
		Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", MessageID: "om-goal-card", Text: text,
		InstanceID: instanceID, ThreadID: threadID,
		CatalogFamilyID: control.FeishuCommandGoal, CatalogVariantID: "goal.codex.normal", CatalogBackend: agentproto.BackendCodex,
		Inbound: &control.ActionInboundMeta{CardDaemonLifecycleID: "lifecycle-1"},
	}
}

func TestGoalCardRejectsMissingOrChangedTarget(t *testing.T) {
	for _, text := range []string{"/goal", "/goal new", "/goal new 新目标", "/goal edit", "/goal edit 改目标", "/goal pause", "/goal resume", "/goal clear", "/goal clear --confirm"} {
		for _, target := range []string{"missing", "thread", "instance", "partial"} {
			if text == "/goal" && target == "missing" {
				continue // The unscoped menu launcher is a read-only query.
			}
			t.Run(fmt.Sprintf("%s/%s", text, target), func(t *testing.T) {
				svc, surfaceID, instanceID := goalInterlockTestSetup(t)
				action := goalCardTestAction(surfaceID, instanceID, "thread-1", text)
				switch target {
				case "missing":
					action.ThreadID, action.InstanceID = "", ""
				case "thread":
					action.ThreadID = "thread-other"
				case "instance":
					action.InstanceID = "instance-other"
				case "partial":
					action.InstanceID = ""
				}
				events := svc.ApplySurfaceAction(action)
				for _, kind := range []agentproto.CommandKind{agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalSet, agentproto.CommandThreadGoalClear} {
					if command := findAgentCommand(events, kind); command != nil {
						t.Fatalf("stale Goal card executed on current target: %#v", command)
					}
				}
				if page := findPageEvent(events); !containsSectionLine(page, "未执行操作") || !containsSectionLine(page, "重新打开") {
					t.Fatalf("stale Goal card must show an error, got %#v", page)
				}
			})
		}
	}
}

func TestGoalMenuLauncherStillQueriesCurrentThread(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(goalCardTestAction(surfaceID, "", "", "/goal"))
	if command := findAgentCommand(events, agentproto.CommandThreadGoalGet); command == nil || command.Target.ThreadID != "thread-1" {
		t.Fatalf("read-only Goal menu launcher must remain usable, got %#v", events)
	}
}

func TestGoalOldEditFormCannotMutateNewlySelectedThread(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	thread := cloneThreadRecord(svc.root.Instances[instanceID].Threads["thread-1"])
	thread.ThreadID = "thread-2"
	svc.root.Instances[instanceID].Threads["thread-2"] = thread
	opened := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal edit"})
	if findPageEvent(opened) == nil {
		t.Fatal("expected original edit form")
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-2"})
	if svc.root.Surfaces[surfaceID].SelectedThreadID != "thread-2" {
		t.Fatal("expected actual thread switch")
	}
	events := svc.ApplySurfaceAction(goalCardTestAction(surfaceID, instanceID, "thread-1", "/goal edit 旧表单目标"))
	if command := findAgentCommand(events, agentproto.CommandThreadGoalSet); command != nil {
		t.Fatalf("old form modified a different thread: %#v", command)
	}
}
