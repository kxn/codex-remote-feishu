package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func steerAllOwnerCardEvent(surfaceID, messageID, title, theme string, sealed bool, lines ...string) control.UIEvent {
	noticeSections := make([]control.FeishuCardTextSection, 0, 1)
	bodyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			bodyLines = append(bodyLines, trimmed)
		}
	}
	if len(bodyLines) != 0 {
		noticeSections = append(noticeSections, control.FeishuCardTextSection{Lines: bodyLines})
	}
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		MessageID:      strings.TrimSpace(messageID),
		Title:          strings.TrimSpace(title),
		ThemeKey:       strings.TrimSpace(theme),
		Interactive:    false,
		NoticeSections: noticeSections,
		Sealed:         sealed,
	})
	return control.UIEvent{
		Kind:             control.UIEventFeishuPageView,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		FeishuPageView:   &view,
	}
}

func steerAllNoopOwnerCardEvent(surfaceID, messageID string) control.UIEvent {
	return steerAllOwnerCardEvent(surfaceID, messageID, "没有可并入的排队输入", "system", true, "当前没有可并入本轮执行的排队消息。")
}

func steerAllRequestedOwnerCardEvent(surfaceID, messageID string, count int) control.UIEvent {
	return steerAllOwnerCardEvent(
		surfaceID,
		messageID,
		"正在并入排队输入",
		"progress",
		false,
		fmt.Sprintf("正在把 %d 条排队输入并入当前执行。", count),
	)
}

func steerAllCompletedOwnerCardEvent(surfaceID, messageID string, count int) control.UIEvent {
	return steerAllOwnerCardEvent(
		surfaceID,
		messageID,
		"已并入排队输入",
		"success",
		true,
		fmt.Sprintf("已把 %d 条排队输入并入当前执行。", count),
	)
}

func steerAllFailedOwnerCardEvent(surfaceID, messageID, text string) control.UIEvent {
	if strings.TrimSpace(text) == "" {
		text = "追加输入失败，已恢复原排队位置。"
	}
	return steerAllOwnerCardEvent(surfaceID, messageID, "并入失败", "error", true, text)
}
