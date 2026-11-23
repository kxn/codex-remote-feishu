package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func targetPickerSelectedSessionDetail(view control.FeishuTargetPickerView) string {
	for _, option := range view.SessionOptions {
		if option.Kind != control.FeishuTargetPickerSessionThread || strings.TrimSpace(option.Value) != strings.TrimSpace(view.SelectedSessionValue) {
			continue
		}
		title := targetPickerDetailLine("完整名称", xutil.FirstNonEmpty(option.FullTitle, view.SelectedSessionLabel, option.Label))
		cwd := targetPickerDetailLine("会话工作目录", option.WorkingDirectory)
		return strings.TrimSpace(title + "\n" + cwd)
	}
	return ""
}

func targetPickerDetailLine(label, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return label + "：" + xutil.TruncateRunes(text, control.FeishuTargetPickerSessionDetailLimit, xutil.TruncateOptions{
		Ellipsis: "…（内容过长，已截断）", ReserveEllipsis: true,
	})
}
