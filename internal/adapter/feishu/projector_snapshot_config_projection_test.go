package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectSnapshotShowsVSCodeNoOverrideAsFollowCurrentState(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind: eventcontract.KindSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode: "vscode",
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				DisplayName: "droid",
				Source:      "vscode",
				RouteMode:   "follow_local",
			},
			NextPrompt: control.PromptRouteSummary{
				CWD:                         "/data/dl/droid",
				EffectiveModel:              "gpt-5.4",
				EffectiveReasoningEffort:    "high",
				EffectiveAccessMode:         "full_access",
				EffectivePlanMode:           "off",
				ObservedThreadPlanMode:      "on",
				UsesLocalRequestedOverrides: true,
				PlanModeOverrideSet:         false,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := renderedV2CardText(t, ops[0])
	if !containsAll(rendered,
		"Plan mode：跟随 VS Code 当前状态",
		"当前会话模式（最近观察）：开启",
		"下条飞书消息：模型 不覆盖，推理 不覆盖，权限 不覆盖，Plan 不覆盖（未覆盖的项目跟随 VS Code 当前状态）",
	) {
		t.Fatalf("expected vscode status projection to show local no-override semantics, got %q", rendered)
	}
	if strings.Contains(rendered, "下条飞书消息：Plan 关闭，模型 gpt-5.4") {
		t.Fatalf("vscode status must not render effective defaults as forced overrides, got %q", rendered)
	}
}

func TestProjectSnapshotShowsObservedThreadAccess(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind: eventcontract.KindSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode: "normal",
			Backend:     "claude",
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				DisplayName: "droid",
				Source:      "headless",
				RouteMode:   "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				CWD:                      "/data/dl/droid",
				EffectiveAccessMode:      "confirm",
				EffectivePlanMode:        "off",
				ObservedThreadAccessMode: "confirm",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := renderedV2CardText(t, ops[0])
	if !containsAll(rendered,
		"当前会话权限（最近观察）：confirm",
		"下条飞书消息：Plan 关闭，模型 未知，推理 未知，权限 confirm",
	) {
		t.Fatalf("expected snapshot to show observed thread access, got %q", rendered)
	}
}
