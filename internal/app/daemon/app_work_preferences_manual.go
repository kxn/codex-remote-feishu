package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
)

func teamManualModelForm(id, model, effort string) map[string]any {
	initial := strings.TrimSpace(model + " " + effort)
	submit := teamSettingsButton(id, "应用手动模型", "settings.model-manual", nil)
	submit["name"], submit["form_action_type"] = "__manual_model_submit", "submit"
	return map[string]any{"tag": "form", "name": "__manual_model_form", "elements": []map[string]any{
		{"tag": "markdown", "content": "也可以手动输入模型名，或「模型名 推理强度」。未出现在列表中的名称由执行后端确认是否支持。"},
		{"tag": "input", "name": "manual_model", "default_value": initial, "placeholder": cardkit.PlainText("模型名，或 模型名 high")}, submit,
	}}
}

func teamSettingsSubmission(elements []teamCardElement, form map[string]any, action, kind string) ([]string, error) {
	if action == "settings.model-manual" {
		if kind != "model" {
			return nil, fmt.Errorf("这张卡片不支持手动模型输入")
		}
		value, ok := form["manual_model"].(string)
		args := strings.Fields(value)
		if !ok || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n") || len(args) < 1 || len(args) > 2 {
			return nil, fmt.Errorf("请输入模型名，或「模型名 推理强度」")
		}
		return append([]string{"settings"}, args...), nil
	}
	value, ok := form["selection"].(string)
	if !ok || !teamSettingsOptionShown(elements, value) {
		return nil, fmt.Errorf("请从这张卡片的下拉菜单选择一个值")
	}
	return []string{"settings", value}, nil
}
