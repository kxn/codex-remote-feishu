package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type ActionHandler func(context.Context, control.Action)

type Gateway interface {
	Start(context.Context, ActionHandler) error
	Apply(context.Context, []Operation) error
}

type NopGateway struct{}

func (NopGateway) Start(context.Context, ActionHandler) error { return nil }
func (NopGateway) Apply(context.Context, []Operation) error   { return nil }

type LiveGatewayConfig struct {
	AppID          string
	AppSecret      string
	TempDir        string
	UseSystemProxy bool
}

type LiveGateway struct {
	config    LiveGatewayConfig
	client    *lark.Client
	wsClient  *ws.Client
	projector *Projector

	mu        sync.Mutex
	reactions map[string]string
	messages  map[string]string
}

func NewLiveGateway(config LiveGatewayConfig) *LiveGateway {
	client := lark.NewClient(config.AppID, config.AppSecret)
	return &LiveGateway{
		config:    config,
		client:    client,
		projector: NewProjector(),
		reactions: map[string]string{},
		messages:  map[string]string{},
	}
}

func (g *LiveGateway) Start(ctx context.Context, handler ActionHandler) error {
	dispatch := dispatcher.NewEventDispatcher("", "")
	dispatch.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		action, ok, err := g.parseMessageEvent(ctx, event)
		if err != nil || !ok {
			return err
		}
		handler(ctx, action)
		return nil
	})
	dispatch.OnP2MessageReactionCreatedV1(func(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) error {
		if event == nil || event.Event == nil || event.Event.MessageId == nil {
			return nil
		}
		g.mu.Lock()
		surfaceSessionID := g.messages[*event.Event.MessageId]
		g.mu.Unlock()
		handler(ctx, control.Action{
			Kind:             control.ActionReactionCreated,
			SurfaceSessionID: surfaceSessionID,
			TargetMessageID:  *event.Event.MessageId,
		})
		return nil
	})
	dispatch.OnP2MessageReactionDeletedV1(func(context.Context, *larkim.P2MessageReactionDeletedV1) error {
		return nil
	})
	dispatch.OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
		if event == nil || event.Event == nil || event.Event.EventKey == nil {
			return nil
		}
		action, ok := menuAction(*event.Event.EventKey)
		if !ok {
			return nil
		}
		operatorID := operatorUserID(event.Event.Operator)
		action.SurfaceSessionID = surfaceIDForInbound("", "p2p", operatorID)
		action.ActorUserID = operatorID
		handler(ctx, action)
		return nil
	})
	g.wsClient = ws.NewClient(g.config.AppID, g.config.AppSecret, ws.WithEventHandler(dispatch))
	return g.wsClient.Start(ctx)
}

func (g *LiveGateway) Apply(ctx context.Context, operations []Operation) error {
	for _, operation := range operations {
		if err := g.applyOne(ctx, operation); err != nil {
			return err
		}
	}
	return nil
}

func (g *LiveGateway) applyOne(ctx context.Context, operation Operation) error {
	switch operation.Kind {
	case OperationSendText:
		body, _ := json.Marshal(map[string]string{"text": operation.Text})
		resp, err := g.client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.ReceiveIdTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(operation.ChatID).
				MsgType("text").
				Content(string(body)).
				Build()).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("send text failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return nil
	case OperationSendCard:
		card, err := json.Marshal(buildCard(operation.CardTitle, operation.CardBody, operation.CardThemeKey))
		if err != nil {
			return err
		}
		resp, err := g.client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.ReceiveIdTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(operation.ChatID).
				MsgType("interactive").
				Content(string(card)).
				Build()).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("send card failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return nil
	case OperationAddReaction:
		if operation.MessageID == "" {
			return nil
		}
		resp, err := g.client.Im.V1.MessageReaction.Create(ctx, larkim.NewCreateMessageReactionReqBuilder().
			MessageId(operation.MessageID).
			Body(larkim.NewCreateMessageReactionReqBodyBuilder().
				ReactionType(larkim.NewEmojiBuilder().EmojiType(operation.EmojiType).Build()).
				Build()).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("add reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		g.mu.Lock()
		if resp.Data != nil && resp.Data.ReactionId != nil {
			g.reactions[reactionKey(operation.MessageID, operation.EmojiType)] = *resp.Data.ReactionId
		}
		g.mu.Unlock()
		return nil
	case OperationRemoveReaction:
		g.mu.Lock()
		reactionID := g.reactions[reactionKey(operation.MessageID, operation.EmojiType)]
		g.mu.Unlock()
		if reactionID == "" {
			return nil
		}
		resp, err := g.client.Im.V1.MessageReaction.Delete(ctx, larkim.NewDeleteMessageReactionReqBuilder().
			MessageId(operation.MessageID).
			ReactionId(reactionID).
			Build())
		if err != nil {
			return err
		}
		if !resp.Success() {
			return fmt.Errorf("remove reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		g.mu.Lock()
		delete(g.reactions, reactionKey(operation.MessageID, operation.EmojiType))
		g.mu.Unlock()
		return nil
	default:
		return nil
	}
}

func (g *LiveGateway) parseMessageEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) (control.Action, bool, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return control.Action{}, false, nil
	}
	message := event.Event.Message
	chatID := stringPtr(message.ChatId)
	chatType := stringPtr(message.ChatType)
	senderUserID := userIDFromMessage(event.Event.Sender)
	surfaceSessionID := surfaceIDForInbound(chatID, chatType, senderUserID)
	action := control.Action{
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		ActorUserID:      senderUserID,
		MessageID:        stringPtr(message.MessageId),
	}

	switch strings.ToLower(stringPtr(message.MessageType)) {
	case "text":
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(stringPtr(message.Content)), &content); err != nil {
			return control.Action{}, false, err
		}
		commandAction, handled := parseTextAction(content.Text)
		if handled {
			commandAction.SurfaceSessionID = surfaceSessionID
			commandAction.ChatID = chatID
			commandAction.ActorUserID = action.ActorUserID
			commandAction.MessageID = action.MessageID
			return commandAction, true, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = content.Text
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	case "image":
		path, mimeType, err := g.downloadImage(ctx, stringPtr(message.MessageId), stringPtr(message.Content))
		if err != nil {
			return control.Action{}, false, err
		}
		action.Kind = control.ActionImageMessage
		action.LocalPath = path
		action.MIMEType = mimeType
		g.recordSurfaceMessage(action.MessageID, surfaceSessionID)
		return action, true, nil
	default:
		return control.Action{}, false, nil
	}
}

func (g *LiveGateway) downloadImage(ctx context.Context, messageID, rawContent string) (string, string, error) {
	var content struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(rawContent), &content); err != nil {
		return "", "", err
	}
	if content.ImageKey == "" {
		return "", "", fmt.Errorf("missing image_key")
	}
	resp, err := g.client.Im.V1.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(content.ImageKey).
		Type("image").
		Build())
	if err != nil {
		return "", "", err
	}
	if !resp.Success() {
		return "", "", fmt.Errorf("download image failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	dir := g.config.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	file, err := os.CreateTemp(dir, "codex-remote-image-*")
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	bytes, err := io.ReadAll(resp.File)
	if err != nil {
		return "", "", err
	}
	if _, err := file.Write(bytes); err != nil {
		return "", "", err
	}
	if err := file.Close(); err != nil {
		return "", "", err
	}
	mimeType := http.DetectContentType(bytes)
	target := file.Name()
	if ext := mimeExtension(mimeType); ext != "" && !strings.HasSuffix(target, ext) {
		renamed := target + ext
		if err := os.Rename(target, renamed); err == nil {
			target = renamed
		}
	}
	return target, mimeType, nil
}

func buildCard(title, body, themeKey string) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
			"enable_forward":   true,
		},
		"header": map[string]any{
			"template": cardTemplate(themeKey, title),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
		},
		"elements": []map[string]any{
			{
				"tag":     "markdown",
				"content": body,
			},
		},
	}
}

func cardTemplate(themeKey, fallback string) string {
	if themeKey == "system" {
		return "wathet"
	}
	templates := []string{"blue", "wathet", "green", "turquoise", "orange", "sunflower"}
	sum := 0
	if themeKey == "" {
		themeKey = fallback
	}
	for _, r := range themeKey {
		sum += int(r)
	}
	return templates[sum%len(templates)]
}

func parseTextAction(text string) (control.Action, bool) {
	trimmed := strings.TrimSpace(text)
	fields := strings.Fields(trimmed)
	if len(fields) > 0 {
		switch strings.ToLower(fields[0]) {
		case "/model":
			return control.Action{Kind: control.ActionModelCommand, Text: trimmed}, true
		case "/reasoning", "/effort":
			return control.Action{Kind: control.ActionReasoningCommand, Text: trimmed}, true
		}
	}
	switch trimmed {
	case "/list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "/status":
		return control.Action{Kind: control.ActionStatus}, true
	case "/stop":
		return control.Action{Kind: control.ActionStop}, true
	case "/threads", "/use", "/sessions":
		return control.Action{Kind: control.ActionShowThreads}, true
	case "/useall", "/sessionsall", "/sessions/all":
		return control.Action{Kind: control.ActionShowAllThreads}, true
	case "/follow":
		return control.Action{Kind: control.ActionFollowLocal}, true
	case "/detach":
		return control.Action{Kind: control.ActionDetach}, true
	default:
		return control.Action{}, false
	}
}

func menuAction(eventKey string) (control.Action, bool) {
	switch eventKey {
	case "list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "status":
		return control.Action{Kind: control.ActionStatus}, true
	case "stop":
		return control.Action{Kind: control.ActionStop}, true
	case "threads", "use", "sessions", "show_threads", "show_sessions":
		return control.Action{Kind: control.ActionShowThreads}, true
	case "threads_all", "useall", "sessions_all", "show_all_threads", "show_all_sessions":
		return control.Action{Kind: control.ActionShowAllThreads}, true
	case "reasonlow", "reason_low", "reason-low":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning low"}, true
	case "reasonmedium", "reason_medium", "reason-medium":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning medium"}, true
	case "reasonhigh", "reason_high", "reason-high":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning high"}, true
	case "reasonxhigh", "reason_xhigh", "reason-xhigh":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning xhigh"}, true
	default:
		return control.Action{}, false
	}
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func surfaceID(chatID, fallbackUserID string) string {
	if chatID != "" {
		return "feishu:chat:" + chatID
	}
	return "feishu:user:" + fallbackUserID
}

func surfaceIDForInbound(chatID, chatType, fallbackUserID string) string {
	if strings.EqualFold(chatType, "p2p") && fallbackUserID != "" {
		return "feishu:user:" + fallbackUserID
	}
	return surfaceID(chatID, fallbackUserID)
}

func userIDFromMessage(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(sender.SenderId.UserId),
		stringPtr(sender.SenderId.OpenId),
		stringPtr(sender.SenderId.UnionId),
	)
}

func operatorUserID(operator *larkapplication.Operator) string {
	if operator == nil || operator.OperatorId == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(operator.OperatorId.UserId),
		stringPtr(operator.OperatorId.OpenId),
		stringPtr(operator.OperatorId.UnionId),
	)
}

func reactionKey(messageID, emojiType string) string {
	return messageID + "|" + emojiType
}

func mimeExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func (g *LiveGateway) recordSurfaceMessage(messageID, surfaceSessionID string) {
	if messageID == "" || surfaceSessionID == "" {
		return
	}
	g.mu.Lock()
	g.messages[messageID] = surfaceSessionID
	g.mu.Unlock()
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func chooseFirst(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
