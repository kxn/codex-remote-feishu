package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func TestTargetPickerHugeTitleFitsBudgetAndKeepsOptions(t *testing.T) {
	name := strings.Repeat("完整的长会话名称", 6000)
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unselected", true: "selected"}[selected], func(t *testing.T) {
			view := control.FeishuTargetPickerView{
				PickerID: "huge-title", Title: "切换工作区与会话", ShowSessionSelect: true,
				ConfirmLabel: "切换", CanConfirm: selected,
				SessionOptions: []control.FeishuTargetPickerSessionOption{
					{Value: "thread:long", Kind: control.FeishuTargetPickerSessionThread, Label: name, MetaText: "原有会话提示"},
					{Value: "thread:normal", Kind: control.FeishuTargetPickerSessionThread, Label: "核对正常会话", MetaText: "正常提示"},
				},
			}
			if selected {
				view.SelectedSessionValue = "thread:long"
				view.SelectedSessionLabel = name
			}
			elements := projectorpkg.TargetPickerElements(view, "life-huge-title")
			size, err := cardtransport.InteractiveMessageCardSize(view.Title, "", projectorpkg.TargetPickerTheme(view), elements, true)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("rendered card: %d bytes", size)
			if size > cardtransport.InteractiveCardTransportLimitBytes {
				t.Errorf("huge title exceeds transport budget: %d bytes", size)
			}
			options := titleBudgetSelectOptions(elements)
			if len(options) != 2 {
				t.Fatalf("huge title must not hide the normal option: got %d options", len(options))
			}
			if options[0]["value"] != "thread:long" || options[1]["value"] != "thread:normal" {
				t.Error("display truncation changed option identity or order")
			}
			longText := cardkit.StringValue(options[0]["text"].(map[string]any)["content"])
			if !strings.Contains(longText, "名称过长，已截断") || !strings.Contains(longText, "原有会话提示") {
				t.Error("long option must disclose truncation and retain its normal hint")
			}
			normalText := cardkit.StringValue(options[1]["text"].(map[string]any)["content"])
			if normalText != "核对正常会话 · 正常提示" {
				t.Errorf("normal option changed: %q", normalText)
			}
			if selected {
				var sawSwitch bool
				for _, action := range cardActionsFromElements(elements) {
					if cardValueMap(action)[frontstagecontract.CardActionPayloadKeyKind] == cardActionKindTargetPickerConfirm && action["disabled"] != true {
						sawSwitch = true
					}
				}
				if !sawSwitch {
					t.Error("enabled switch button disappeared")
				}
			}
			if view.SessionOptions[0].Label != name || (selected && view.SelectedSessionLabel != name) {
				t.Error("rendering must preserve the complete semantic name")
			}
		})
	}
}

func TestTargetPickerHugeTitleKeepsPaginationAndSwitchButton(t *testing.T) {
	options := []control.FeishuTargetPickerSessionOption{
		{Value: "thread:long", Kind: control.FeishuTargetPickerSessionThread, Label: strings.Repeat("完整的长会话名称", 6000)},
	}
	for i := 0; i < 180; i++ {
		options = append(options, control.FeishuTargetPickerSessionOption{
			Value: "thread:normal-" + leftPad3(i), Kind: control.FeishuTargetPickerSessionThread,
			Label: "正常会话 " + leftPad3(i), MetaText: strings.Repeat("原有提示", 60),
		})
	}
	view := control.FeishuTargetPickerView{
		PickerID: "huge-title-page", Title: "切换工作区与会话", ShowSessionSelect: true,
		SelectedSessionValue: "thread:long", ConfirmLabel: "切换", CanConfirm: true, SessionOptions: options,
	}
	elements := projectorpkg.TargetPickerElements(view, "life-huge-title-page")
	size, err := cardtransport.InteractiveMessageCardSize(view.Title, "", projectorpkg.TargetPickerTheme(view), elements, true)
	if err != nil {
		t.Fatal(err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Errorf("paginated huge title exceeds transport budget: %d bytes", size)
	}
	shown := titleBudgetSelectOptions(elements)
	if len(shown) < 2 || shown[0]["value"] != "thread:long" || shown[1]["value"] != "thread:normal-000" {
		t.Error("long title must not remove itself or the next normal option")
	}
	var sawPage, sawSwitch bool
	for _, action := range cardActionsFromElements(elements) {
		switch cardValueMap(action)[frontstagecontract.CardActionPayloadKeyKind] {
		case cardActionKindTargetPickerPage:
			sawPage = true
		case cardActionKindTargetPickerConfirm:
			sawSwitch = action["disabled"] != true
		}
	}
	if !sawPage || !sawSwitch {
		t.Errorf("long title must preserve pagination and switch actions: page=%v switch=%v", sawPage, sawSwitch)
	}
}

func titleBudgetSelectOptions(value any) []map[string]any {
	var options []map[string]any
	switch typed := value.(type) {
	case map[string]any:
		if typed["tag"] == "select_static" {
			return cardMapArray(typed["options"])
		}
		for _, child := range typed {
			options = append(options, titleBudgetSelectOptions(child)...)
		}
	case []map[string]any:
		for _, child := range typed {
			options = append(options, titleBudgetSelectOptions(child)...)
		}
	case []any:
		for _, child := range typed {
			options = append(options, titleBudgetSelectOptions(child)...)
		}
	}
	return options
}
