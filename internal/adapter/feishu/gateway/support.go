package gateway

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	inboundMessageParseTimeout = 30 * time.Second
)

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuPostContent struct {
	Title   string             `json:"title"`
	Content [][]feishuPostNode `json:"content"`
}

type feishuPostNode struct {
	Tag       string `json:"tag"`
	Text      string `json:"text"`
	Href      string `json:"href"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	ImageKey  string `json:"image_key"`
	EmojiType string `json:"emoji_type"`
	Language  string `json:"language"`
}

func NewFeishuTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = parent
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func ReferencedMessageID(message *larkim.EventMessage) string {
	if message == nil {
		return ""
	}
	targetMessageID := strings.TrimSpace(xutil.StringValue(message.ParentId))
	if targetMessageID == "" {
		targetMessageID = strings.TrimSpace(xutil.StringValue(message.RootId))
	}
	return targetMessageID
}

// NormalizeGatewayID trims surrounding whitespace from a gateway id. It
// consolidates the normalizeGatewayID copies previously living in the feishu
// and preview packages.
func NormalizeGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func parseFeishuEventText(rawContent string, mentions []*larkim.MentionEvent) (displayText string, commandText string, err error) {
	rawText, err := ParseTextContent(rawContent)
	if err != nil {
		return "", "", err
	}
	return normalizeFeishuTextMentions(rawText, mentions), normalizeFeishuCommandCandidate(rawText, mentions), nil
}

func groupMessageMentionGateReason(env InboundEnv, message *larkim.EventMessage, senderType string) string {
	if message == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(xutil.StringValue(message.ChatType)), "p2p") {
		return ""
	}
	botOpenID := strings.TrimSpace(env.BotOpenID)
	if botOpenID == "" {
		return "bot_identity_unavailable"
	}
	if len(message.Mentions) == 0 {
		return unmentionedGroupMessageGateReason(env, message, senderType)
	}
	for _, mention := range message.Mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		if strings.TrimSpace(xutil.StringValue(mention.Id.OpenId)) == botOpenID {
			return ""
		}
	}
	return "ignored_not_mentioned_current_bot"
}

func unmentionedGroupMessageGateReason(env InboundEnv, message *larkim.EventMessage, senderType string) string {
	if messageSenderIsBot(senderType) {
		return "ignored_unmentioned_bot_sender"
	}
	if env.PrimaryGatewayForChat == nil {
		return "ignored_no_primary_gateway_lookup"
	}
	chatID := strings.TrimSpace(xutil.StringValue(message.ChatId))
	primaryGatewayID := strings.TrimSpace(env.PrimaryGatewayForChat(chatID))
	if primaryGatewayID == "" {
		return "ignored_no_primary_gateway"
	}
	if primaryGatewayID != strings.TrimSpace(env.GatewayID) {
		return "ignored_not_primary_gateway"
	}
	return ""
}

func messageSenderIsBot(senderType string) bool {
	switch strings.ToLower(strings.TrimSpace(senderType)) {
	case "app", "bot":
		return true
	default:
		return false
	}
}

func normalizeFeishuTextMentions(rawText string, mentions []*larkim.MentionEvent) string {
	replacements := feishuMentionReplacements(mentions)
	if len(replacements) == 0 {
		return rawText
	}
	pairs := make([]string, 0, len(replacements)*2)
	for _, item := range replacements {
		pairs = append(pairs, item.key, item.label)
	}
	return strings.NewReplacer(pairs...).Replace(rawText)
}

func normalizeFeishuCommandCandidate(rawText string, mentions []*larkim.MentionEvent) string {
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" {
		return rawText
	}
	if commandText, ok := stripLeadingFeishuMentionKeys(trimmed, feishuMentionKeys(mentions)); ok {
		return commandText
	}
	if len(mentions) == 0 && !strings.Contains(trimmed, "@_user_") {
		return rawText
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return rawText
	}
	index := 0
	for index < len(fields) && strings.HasPrefix(fields[index], "@") {
		index++
	}
	if index > 0 && index < len(fields) && strings.HasPrefix(fields[index], "/") {
		return strings.Join(fields[index:], " ")
	}
	return rawText
}

func stripLeadingFeishuMentionKeys(text string, keys []string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	rest := strings.TrimSpace(text)
	stripped := false
	for {
		matched := false
		for _, key := range keys {
			if !strings.HasPrefix(rest, key) {
				continue
			}
			next := strings.TrimLeft(rest[len(key):], " \t\r\n")
			if next == "" {
				return "", false
			}
			rest = next
			stripped = true
			matched = true
			break
		}
		if !matched {
			break
		}
	}
	if stripped && strings.HasPrefix(rest, "/") {
		return rest, true
	}
	return "", false
}

func isPureCurrentBotMentionText(rawText string, mentions []*larkim.MentionEvent, botOpenID string) bool {
	botOpenID = strings.TrimSpace(botOpenID)
	if botOpenID == "" || len(mentions) == 0 {
		return false
	}
	keys := currentBotMentionKeys(mentions, botOpenID)
	if len(keys) == 0 {
		return false
	}
	rest := strings.TrimSpace(rawText)
	stripped := false
	for rest != "" {
		matched := false
		for _, key := range keys {
			if !strings.HasPrefix(rest, key) {
				continue
			}
			rest = strings.TrimSpace(rest[len(key):])
			stripped = true
			matched = true
			break
		}
		if !matched {
			return false
		}
	}
	return stripped
}

func currentBotMentionKeys(mentions []*larkim.MentionEvent, botOpenID string) []string {
	botOpenID = strings.TrimSpace(botOpenID)
	keys := make([]string, 0, len(mentions))
	seen := map[string]struct{}{}
	for _, mention := range mentions {
		if mention == nil || mention.Id == nil {
			continue
		}
		if strings.TrimSpace(xutil.StringValue(mention.Id.OpenId)) != botOpenID {
			continue
		}
		key := strings.TrimSpace(xutil.StringValue(mention.Key))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return len(keys[i]) > len(keys[j])
	})
	return keys
}

type feishuMentionReplacement struct {
	key   string
	label string
}

func feishuMentionKeys(mentions []*larkim.MentionEvent) []string {
	replacements := feishuMentionReplacements(mentions)
	keys := make([]string, 0, len(replacements))
	for _, item := range replacements {
		keys = append(keys, item.key)
	}
	return keys
}

func feishuMentionReplacements(mentions []*larkim.MentionEvent) []feishuMentionReplacement {
	replacements := make([]feishuMentionReplacement, 0, len(mentions))
	seen := map[string]struct{}{}
	for _, mention := range mentions {
		if mention == nil {
			continue
		}
		key := strings.TrimSpace(xutil.StringValue(mention.Key))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		replacements = append(replacements, feishuMentionReplacement{
			key:   key,
			label: feishuMentionDisplayLabel(mention, key),
		})
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return len(replacements[i].key) > len(replacements[j].key)
	})
	return replacements
}

func feishuMentionDisplayLabel(mention *larkim.MentionEvent, fallback string) string {
	if mention == nil {
		return fallback
	}
	name := strings.TrimSpace(xutil.StringValue(mention.Name))
	if name == "" {
		return fallback
	}
	if strings.HasPrefix(name, "@") {
		return name
	}
	return "@" + name
}
