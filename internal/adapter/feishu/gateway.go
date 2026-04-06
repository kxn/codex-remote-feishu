package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
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
	GatewayID      string
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
	config.GatewayID = normalizeGatewayID(config.GatewayID)
	client := lark.NewClient(config.AppID, config.AppSecret)
	return &LiveGateway{
		config:    config,
		client:    client,
		projector: NewProjector(),
		reactions: map[string]string{},
		messages:  map[string]string{},
	}
}

func (g *LiveGateway) Client() *lark.Client {
	if g == nil {
		return nil
	}
	return g.client
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
	dispatch.OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
		action, ok := g.parseMessageRecalledEvent(event)
		if !ok {
			return nil
		}
		handler(ctx, action)
		return nil
	})
	dispatch.OnP2CardActionTrigger(func(ctx context.Context, event *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, error) {
		action, ok := g.parseCardActionTriggerEvent(event)
		if ok {
			handler(ctx, action)
		}
		return &larkcallback.CardActionTriggerResponse{}, nil
	})
	dispatch.OnP2BotMenuV6(func(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
		if event == nil || event.Event == nil || event.Event.EventKey == nil {
			return nil
		}
		rawKey := *event.Event.EventKey
		action, ok := menuAction(rawKey)
		if !ok {
			log.Printf("feishu bot menu ignored: raw_key=%q normalized=%q", rawKey, normalizeMenuEventKey(rawKey))
			return nil
		}
		log.Printf("feishu bot menu handled: raw_key=%q normalized=%q action=%s", rawKey, normalizeMenuEventKey(rawKey), action.Kind)
		operatorID := operatorUserID(event.Event.Operator)
		action.GatewayID = g.config.GatewayID
		action.SurfaceSessionID = surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
		action.ActorUserID = operatorID
		handler(ctx, action)
		return nil
	})
	g.wsClient = ws.NewClient(g.config.AppID, g.config.AppSecret, ws.WithEventHandler(dispatch))
	return g.wsClient.Start(ctx)
}

func (g *LiveGateway) Apply(ctx context.Context, operations []Operation) error {
	for _, operation := range operations {
		if operation.GatewayID != "" && normalizeGatewayID(operation.GatewayID) != g.config.GatewayID {
			return fmt.Errorf("gateway apply mismatch: operation gateway=%s gateway=%s", operation.GatewayID, g.config.GatewayID)
		}
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
		receiveID, receiveIDType := operation.ReceiveID, operation.ReceiveIDType
		if receiveID == "" || receiveIDType == "" {
			receiveID, receiveIDType = ResolveReceiveTarget(operation.ChatID, "")
		}
		if receiveID == "" || receiveIDType == "" {
			return fmt.Errorf("send text failed: missing receive target")
		}
		resp, err := g.client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(receiveIDType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(receiveID).
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
		if resp.Data != nil {
			g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
		}
		return nil
	case OperationSendCard:
		card, err := json.Marshal(buildCard(operation.CardTitle, operation.CardBody, operation.CardThemeKey, operation.CardElements))
		if err != nil {
			return err
		}
		receiveID, receiveIDType := operation.ReceiveID, operation.ReceiveIDType
		if receiveID == "" || receiveIDType == "" {
			receiveID, receiveIDType = ResolveReceiveTarget(operation.ChatID, "")
		}
		if receiveID == "" || receiveIDType == "" {
			return fmt.Errorf("send card failed: missing receive target")
		}
		resp, err := g.client.Im.V1.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(receiveIDType).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(receiveID).
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
		if resp.Data != nil {
			g.recordSurfaceMessage(stringPtr(resp.Data.MessageId), operation.SurfaceSessionID)
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
	surfaceSessionID := surfaceIDForInbound(g.config.GatewayID, chatID, chatType, senderUserID)
	action := control.Action{
		GatewayID:        g.config.GatewayID,
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

func (g *LiveGateway) parseMessageRecalledEvent(event *larkim.P2MessageRecalledV1) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.MessageId == nil {
		return control.Action{}, false
	}
	messageID := strings.TrimSpace(*event.Event.MessageId)
	if messageID == "" {
		return control.Action{}, false
	}
	g.mu.Lock()
	surfaceSessionID := g.messages[messageID]
	g.mu.Unlock()
	if surfaceSessionID == "" {
		return control.Action{}, false
	}
	return control.Action{
		Kind:             control.ActionMessageRecalled,
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           strings.TrimSpace(stringPtr(event.Event.ChatId)),
		TargetMessageID:  messageID,
	}, true
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

func buildCard(title, body, themeKey string, extraElements []map[string]any) map[string]any {
	elements := make([]map[string]any, 0, len(extraElements)+1)
	if strings.TrimSpace(body) != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": body,
		})
	}
	elements = append(elements, extraElements...)
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
		"elements": elements,
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

func (g *LiveGateway) parseCardActionTriggerEvent(event *larkcallback.CardActionTriggerEvent) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return control.Action{}, false
	}
	value := event.Event.Action.Value
	kind := strings.TrimSpace(stringMapValue(value, "kind"))
	if kind == "" {
		return control.Action{}, false
	}

	operatorID := operatorUserIDFromCard(event.Event.Operator)
	chatID := ""
	messageID := ""
	if event.Event.Context != nil {
		chatID = strings.TrimSpace(event.Event.Context.OpenChatID)
		messageID = strings.TrimSpace(event.Event.Context.OpenMessageID)
	}
	surfaceSessionID := g.surfaceForCardAction(messageID, chatID, operatorID)
	if surfaceSessionID == "" {
		return control.Action{}, false
	}

	switch kind {
	case "prompt_select":
		promptID := strings.TrimSpace(stringMapValue(value, "prompt_id"))
		optionID := strings.TrimSpace(stringMapValue(value, "option_id"))
		if promptID == "" || optionID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionSelectPrompt,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PromptID:         promptID,
			OptionID:         optionID,
		}, true
	case "request_respond":
		requestID := strings.TrimSpace(stringMapValue(value, "request_id"))
		if requestID == "" {
			return control.Action{}, false
		}
		optionID := strings.TrimSpace(stringMapValue(value, "request_option_id"))
		if optionID == "" {
			if value["approved"] != nil {
				if boolMapValue(value, "approved") {
					optionID = "accept"
				} else {
					optionID = "decline"
				}
			}
		}
		return control.Action{
			Kind:             control.ActionRespondRequest,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			RequestID:        requestID,
			RequestType:      strings.TrimSpace(stringMapValue(value, "request_type")),
			RequestOptionID:  optionID,
			Approved:         boolMapValue(value, "approved"),
		}, true
	default:
		return control.Action{}, false
	}
}

func (g *LiveGateway) surfaceForCardAction(messageID, chatID, operatorID string) string {
	if operatorID != "" {
		return surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
	}
	if messageID != "" {
		g.mu.Lock()
		surfaceSessionID := g.messages[messageID]
		g.mu.Unlock()
		if surfaceSessionID != "" {
			return surfaceSessionID
		}
	}
	if chatID != "" {
		return surfaceID(g.config.GatewayID, chatID, "")
	}
	return ""
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
		case "/access", "/approval":
			return control.Action{Kind: control.ActionAccessCommand, Text: trimmed}, true
		}
	}
	switch trimmed {
	case "/list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "/status":
		return control.Action{Kind: control.ActionStatus}, true
	case "/stop":
		return control.Action{Kind: control.ActionStop}, true
	case "/newinstance":
		return control.Action{Kind: control.ActionNewInstance}, true
	case "/killinstance":
		return control.Action{Kind: control.ActionKillInstance}, true
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
	if action, ok := dynamicMenuAction(eventKey); ok {
		return action, true
	}
	switch normalizeMenuEventKey(eventKey) {
	case "list":
		return control.Action{Kind: control.ActionListInstances}, true
	case "status":
		return control.Action{Kind: control.ActionStatus}, true
	case "stop":
		return control.Action{Kind: control.ActionStop}, true
	case "newinstance":
		return control.Action{Kind: control.ActionNewInstance}, true
	case "killinstance":
		return control.Action{Kind: control.ActionKillInstance}, true
	case "threads", "use", "sessions", "showthreads", "showsessions":
		return control.Action{Kind: control.ActionShowThreads}, true
	case "threadsall", "useall", "sessionsall", "showallthreads", "showallsessions":
		return control.Action{Kind: control.ActionShowAllThreads}, true
	case "reasonlow", "reason_low", "reason-low":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning low"}, true
	case "reasonmedium", "reason_medium", "reason-medium":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning medium"}, true
	case "reasonhigh", "reason_high", "reason-high":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning high"}, true
	case "reasonxhigh", "reason_xhigh", "reason-xhigh":
		return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning xhigh"}, true
	case "accessfull", "approvalfull":
		return control.Action{Kind: control.ActionAccessCommand, Text: "/access full"}, true
	case "accessconfirm", "approvalconfirm":
		return control.Action{Kind: control.ActionAccessCommand, Text: "/access confirm"}, true
	default:
		return control.Action{}, false
	}
}

func dynamicMenuAction(eventKey string) (control.Action, bool) {
	trimmed := strings.TrimSpace(eventKey)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"model_", "model-"} {
		if strings.HasPrefix(lower, prefix) {
			model := strings.TrimSpace(trimmed[len(prefix):])
			if model == "" {
				return control.Action{}, false
			}
			return control.Action{Kind: control.ActionModelCommand, Text: "/model " + model}, true
		}
	}
	for _, prefix := range []string{"reason_", "reason-"} {
		if strings.HasPrefix(lower, prefix) {
			effort := strings.ToLower(strings.TrimSpace(trimmed[len(prefix):]))
			if !menuReasoningEffort(effort) {
				return control.Action{}, false
			}
			return control.Action{Kind: control.ActionReasoningCommand, Text: "/reasoning " + effort}, true
		}
	}
	return control.Action{}, false
}

func menuReasoningEffort(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func normalizeMenuEventKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func surfaceID(gatewayID, chatID, fallbackUserID string) string {
	if chatID != "" {
		return SurfaceRef{
			Platform:  PlatformFeishu,
			GatewayID: normalizeGatewayID(gatewayID),
			ScopeKind: ScopeKindChat,
			ScopeID:   strings.TrimSpace(chatID),
		}.SurfaceID()
	}
	return SurfaceRef{
		Platform:  PlatformFeishu,
		GatewayID: normalizeGatewayID(gatewayID),
		ScopeKind: ScopeKindUser,
		ScopeID:   strings.TrimSpace(fallbackUserID),
	}.SurfaceID()
}

func surfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID string) string {
	if strings.EqualFold(chatType, "p2p") && fallbackUserID != "" {
		return surfaceID(gatewayID, "", fallbackUserID)
	}
	return surfaceID(gatewayID, chatID, fallbackUserID)
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

func operatorUserIDFromCard(operator *larkcallback.Operator) string {
	if operator == nil {
		return ""
	}
	return chooseFirst(
		stringPtr(operator.UserID),
		strings.TrimSpace(operator.OpenID),
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

func stringMapValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch current := value.(type) {
	case string:
		return current
	case fmt.Stringer:
		return current.String()
	default:
		return fmt.Sprint(current)
	}
}

func boolMapValue(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	current, _ := value.(bool)
	return current
}

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	if strings.TrimSpace(chatID) != "" {
		return chatID, larkim.ReceiveIdTypeChatId
	}
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return "", ""
	}
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		return actorUserID, larkim.ReceiveIdTypeOpenId
	case strings.HasPrefix(actorUserID, "on_"):
		return actorUserID, larkim.ReceiveIdTypeUnionId
	default:
		return actorUserID, larkim.ReceiveIdTypeUserId
	}
}
