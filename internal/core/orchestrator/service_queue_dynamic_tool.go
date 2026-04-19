package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) renderDynamicToolCallItem(instanceID string, event agentproto.Event) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	events := []control.UIEvent{}
	if surface.ActiveTurnOrigin != agentproto.InitiatorLocalUI {
		routeMode := surface.RouteMode
		if routeMode != state.RouteModeFollowLocal {
			routeMode = state.RouteModePinned
		}
		if inst != nil {
			events = append(events, s.bindSurfaceToThreadMode(surface, inst, event.ThreadID, routeMode)...)
		}
	}

	imageItems := dynamicToolImageItemsFromMetadata(event.Metadata)
	imageLinks := dynamicToolImageLinksFromMetadata(event.Metadata)
	text := dynamicToolSummaryTextFromMetadata(event.Metadata, len(imageItems), imageLinks)
	if text != "" {
		events = append(events, s.renderTextItem(instanceID, event.ThreadID, event.TurnID, event.ItemID, text, false)...)
	}
	return events
}

func dynamicToolImageItemsFromMetadata(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["contentItems"]
	if !ok || raw == nil {
		return nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil
	}
	images := make([]string, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(lookupStringFromAny(record["type"])), "image") {
			continue
		}
		image := firstNonEmpty(lookupStringFromAny(record["imageBase64"]))
		if image == "" {
			continue
		}
		images = append(images, image)
	}
	return images
}

func dynamicToolImageLinksFromMetadata(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["contentItems"]
	if !ok || raw == nil {
		return nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []map[string]any:
		values = make([]any, 0, len(typed))
		for _, item := range typed {
			values = append(values, item)
		}
	default:
		return nil
	}
	links := make([]string, 0, len(values))
	for _, value := range values {
		record, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(lookupStringFromAny(record["type"])), "image") {
			continue
		}
		if strings.TrimSpace(lookupStringFromAny(record["imageBase64"])) != "" {
			continue
		}
		link := strings.TrimSpace(lookupStringFromAny(record["url"]))
		if link == "" {
			continue
		}
		links = append(links, link)
	}
	return links
}

func dynamicToolSummaryTextFromMetadata(metadata map[string]any, imageCount int, imageLinks []string) string {
	text := strings.TrimSpace(metadataString(metadata, "text"))
	tool := strings.TrimSpace(metadataString(metadata, "tool"))
	linkSummary := ""
	if len(imageLinks) != 0 {
		linkSummary = "\n\n图片链接：\n" + strings.Join(imageLinks, "\n")
	}
	if text != "" {
		if tool != "" {
			return fmt.Sprintf("工具 `%s` 返回：\n\n%s%s", tool, text, linkSummary)
		}
		return text + linkSummary
	}
	if imageCount == 0 && len(imageLinks) == 0 {
		return ""
	}
	switch {
	case tool != "" && imageCount == 1:
		return fmt.Sprintf("工具 `%s` 返回了 1 张图片。%s", tool, linkSummary)
	case tool != "" && imageCount > 1:
		return fmt.Sprintf("工具 `%s` 返回了 %d 张图片。%s", tool, imageCount, linkSummary)
	case tool != "" && len(imageLinks) != 0:
		return fmt.Sprintf("工具 `%s` 返回了图片链接。%s", tool, linkSummary)
	case imageCount == 1:
		return "工具调用返回了 1 张图片。" + linkSummary
	default:
		if imageCount > 1 {
			return fmt.Sprintf("工具调用返回了 %d 张图片。%s", imageCount, linkSummary)
		}
		return "工具调用返回了图片链接。" + linkSummary
	}
}
