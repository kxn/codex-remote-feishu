package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type steerSupplementSummary struct {
	TextParts  []string
	ImageCount int
	FileCount  int
}

type forwardedChatEnvelope struct {
	Root forwardedChatNode `json:"root"`
}

type forwardedChatNode struct {
	Kind        string              `json:"kind"`
	MessageType string              `json:"message_type,omitempty"`
	Items       []forwardedChatNode `json:"items,omitempty"`
}

func (s *Service) steerUserSupplementEvent(surface *state.SurfaceConsoleRecord, binding *pendingSteerBinding) *control.UIEvent {
	if surface == nil || binding == nil {
		return nil
	}
	replyToMessageID, replyToMessagePreview := s.replyAnchorForTurn(binding.InstanceID, binding.ThreadID, binding.TurnID)
	if strings.TrimSpace(replyToMessageID) == "" {
		return nil
	}
	summary := summarizePendingSteerInputs(surface, binding)
	text := formatSteerUserSupplementText(summary)
	if text == "" {
		return nil
	}
	return &control.UIEvent{
		Kind:                 control.UIEventTimelineText,
		SurfaceSessionID:     surface.SurfaceSessionID,
		SourceMessageID:      replyToMessageID,
		SourceMessagePreview: replyToMessagePreview,
		TimelineText: &control.TimelineText{
			ThreadID:              binding.ThreadID,
			TurnID:                binding.TurnID,
			Type:                  control.TimelineTextSteerUserSupplement,
			Text:                  text,
			ReplyToMessageID:      replyToMessageID,
			ReplyToMessagePreview: replyToMessagePreview,
		},
	}
}

func summarizePendingSteerInputs(surface *state.SurfaceConsoleRecord, binding *pendingSteerBinding) steerSupplementSummary {
	if surface == nil || binding == nil {
		return steerSupplementSummary{}
	}
	summary := steerSupplementSummary{}
	for _, queueItemID := range pendingSteerQueueItemIDs(binding) {
		item := surface.QueueItems[queueItemID]
		if item == nil {
			continue
		}
		for _, input := range queueItemSteerInputs(item) {
			switch input.Type {
			case agentproto.InputLocalImage, agentproto.InputRemoteImage:
				summary.ImageCount++
			case agentproto.InputText:
				text := strings.TrimSpace(input.Text)
				if text == "" {
					continue
				}
				summary.FileCount += countStructuredInputFiles(text)
				if inputTextHiddenFromSteerSupplement(text) {
					continue
				}
				summary.TextParts = append(summary.TextParts, text)
			}
		}
	}
	return summary
}

func formatSteerUserSupplementText(summary steerSupplementSummary) string {
	text := strings.TrimSpace(strings.Join(summary.TextParts, "\n\n"))
	attachmentSuffix := steerSupplementAttachmentSuffix(summary.ImageCount, summary.FileCount)
	switch {
	case text != "" && attachmentSuffix != "":
		return "用户补充：" + text + "（追加 " + attachmentSuffix + "）"
	case text != "":
		return "用户补充：" + text
	case attachmentSuffix != "":
		return "用户补充（追加 " + attachmentSuffix + "）"
	default:
		return ""
	}
}

func steerSupplementAttachmentSuffix(imageCount, fileCount int) string {
	parts := []string{}
	if imageCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 张图片", imageCount))
	}
	if fileCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个文件", fileCount))
	}
	return strings.Join(parts, "，")
}

func inputTextHiddenFromSteerSupplement(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if _, ok := unwrapTaggedInputBody(text, "forwarded_chat_bundle_v1"); ok {
		return true
	}
	if _, ok := unwrapTaggedInputBody(text, "quoted_forwarded_chat_bundle_v1"); ok {
		return true
	}
	if _, ok := unwrapTaggedInputBody(text, "被引用内容"); ok {
		return true
	}
	return false
}

func countStructuredInputFiles(text string) int {
	for _, tag := range []string{"forwarded_chat_bundle_v1", "quoted_forwarded_chat_bundle_v1"} {
		body, ok := unwrapTaggedInputBody(text, tag)
		if !ok {
			continue
		}
		return countForwardedChatFileNodes(body)
	}
	return 0
}

func unwrapTaggedInputBody(text, tag string) (string, bool) {
	text = strings.TrimSpace(text)
	tag = strings.TrimSpace(tag)
	if text == "" || tag == "" {
		return "", false
	}
	prefix := "<" + tag + ">"
	suffix := "</" + tag + ">"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return "", false
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix))
	if body == "" {
		return "", false
	}
	return body, true
}

func countForwardedChatFileNodes(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var envelope forwardedChatEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return 0
	}
	return countForwardedChatFileNodesFromNode(envelope.Root)
}

func countForwardedChatFileNodesFromNode(node forwardedChatNode) int {
	count := 0
	if strings.EqualFold(strings.TrimSpace(node.Kind), "message") && strings.EqualFold(strings.TrimSpace(node.MessageType), "file") {
		count++
	}
	for _, child := range node.Items {
		count += countForwardedChatFileNodesFromNode(child)
	}
	return count
}
