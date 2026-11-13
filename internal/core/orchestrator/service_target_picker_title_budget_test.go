package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTargetPickerHugeTitleStageCardsFitBudget(t *testing.T) {
	name := strings.Repeat("完整的长会话名称", 6000)
	for _, stage := range []control.FeishuTargetPickerStage{control.FeishuTargetPickerStageProcessing, control.FeishuTargetPickerStageSucceeded} {
		t.Run(string(stage), func(t *testing.T) {
			view := control.FeishuTargetPickerView{
				PickerID: "huge-title", Title: "切换工作区与会话", Stage: stage,
				SelectedSessionValue: "thread:real-thread", SelectedSessionLabel: name,
				BodySections: targetPickerBodySections(control.FeishuTargetPickerPageTarget, "web", "", name, "原有会话提示", "", "", "", "", "", ""),
			}
			if stage == control.FeishuTargetPickerStageProcessing {
				view.StatusSections = targetPickerSwitchProcessingStatus("web", name).Sections
			}
			elements := projectorpkg.TargetPickerElements(view, "life-huge-title")
			size, err := cardtransport.InteractiveMessageCardSize(view.Title, "", projectorpkg.TargetPickerTheme(view), elements, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("rendered stage card: %d bytes", size)
			if size > cardtransport.InteractiveCardTransportLimitBytes {
				t.Errorf("huge title exceeds stage-card transport budget: %d bytes", size)
			}
			var sawNotice, sawHint bool
			for _, element := range elements {
				text, _ := element["text"].(map[string]any)
				content := cardkit.StringValue(text["content"])
				sawNotice = sawNotice || strings.Contains(content, "名称过长，已截断")
				sawHint = sawHint || strings.Contains(content, "原有会话提示")
			}
			if !sawNotice || !sawHint {
				t.Error("stage card must disclose truncation and retain its normal hint")
			}
			if view.SelectedSessionLabel != name || view.SelectedSessionValue != "thread:real-thread" {
				t.Error("display projection must preserve the complete name and selected identity")
			}
		})
	}
}

func TestTargetPickerHugeTitleConfirmPreservesRuntimeNameAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 31, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	name := strings.Repeat("完整的长会话名称", 6000)
	const threadID = "real-target-thread"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-web", WorkspaceRoot: "/data/web", WorkspaceKey: "/data/web", Online: true,
		Threads: map[string]*state.ThreadRecord{
			threadID: {ThreadID: threadID, Name: name, CWD: "/data/web", Loaded: true, LastUsedAt: now},
		},
	})
	opened := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionShowAllThreads, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1",
	}))
	confirmed := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1",
		PickerID: opened.PickerID, WorkspaceKey: "/data/web", TargetPickerValue: targetPickerThreadValue(threadID),
	}))
	if confirmed.Stage != control.FeishuTargetPickerStageSucceeded || svc.root.Surfaces["surface-1"].SelectedThreadID != threadID {
		t.Fatal("confirm must still attach the real target thread")
	}
	if confirmed.SelectedSessionValue != targetPickerThreadValue(threadID) || svc.root.Instances["inst-web"].Threads[threadID].Name != name {
		t.Fatal("display truncation must preserve the selected value and complete runtime name")
	}
}
