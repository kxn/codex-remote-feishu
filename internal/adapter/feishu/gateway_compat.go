package feishu

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type inboundWork interface {
	enqueue(*surfaceInboundLane) bool
}

type surfaceInboundLane struct {
	inner *gatewaypkg.SurfaceInboundLane
}

type queuedMessageWork struct {
	inner *gatewaypkg.QueuedMessageWork
}

type queuedActionWork struct {
	action control.Action
}

type plannedInboundMessage struct {
	action *control.Action
	queue  *queuedMessageWork
}

func newSurfaceInboundLane(ctx context.Context, gateway *LiveGateway, handler ActionHandler) *surfaceInboundLane {
	return &surfaceInboundLane{
		inner: gatewaypkg.NewSurfaceInboundLane(ctx, gateway.inboundEnv(), gatewayDispatcher(handler)),
	}
}

func (l *surfaceInboundLane) enqueue(work inboundWork) bool {
	if l == nil || work == nil {
		return false
	}
	return work.enqueue(l)
}

func (l *surfaceInboundLane) markActionDuplicate(action control.Action) bool {
	if l == nil || l.inner == nil {
		return false
	}
	return l.inner.MarkActionDuplicate(action)
}

func (w *queuedMessageWork) enqueue(l *surfaceInboundLane) bool {
	if l == nil || l.inner == nil || w == nil || w.inner == nil {
		return false
	}
	return l.inner.EnqueueQueuedMessage(w.inner)
}

func (w *queuedActionWork) enqueue(l *surfaceInboundLane) bool {
	if l == nil || l.inner == nil || w == nil {
		return false
	}
	return l.inner.EnqueueAction(w.action)
}

func gatewayDispatcher(handler ActionHandler) gatewaypkg.ActionDispatcher {
	if handler == nil {
		return nil
	}
	return func(ctx context.Context, action control.Action) error {
		return handleGatewayEventAction(ctx, action, handler)
	}
}

func (g *LiveGateway) routingEnv() gatewaypkg.RoutingEnv {
	return gatewaypkg.RoutingEnv{
		GatewayID:            g.config.GatewayID,
		SurfaceForCardAction: g.surfaceForCardAction,
	}
}

func (g *LiveGateway) inboundEnv() gatewaypkg.InboundEnv {
	return gatewaypkg.InboundEnv{
		GatewayID:            g.config.GatewayID,
		LookupSurfaceMessage: g.lookupSurfaceMessage,
		ParseTextAction:      parseTextAction,
		QuotedInputs:             g.quotedInputs,
		ParsePostInputs:          g.parsePostInputs,
		BuildMergeForwardStructuredInput: func(ctx context.Context, message *larkim.EventMessage) (string, []agentproto.Input, error) {
			payload, err := g.buildMergeForwardStructuredPayloadFromEvent(ctx, message)
			if err != nil {
				return "", nil, err
			}
			return payload.Summary, payload.Inputs, nil
		},
		RecordSurfaceMessage:       g.recordSurfaceMessage,
		DownloadImage:              g.downloadImageFn,
		DownloadFile:               g.downloadFileFn,
		DeliverAsyncInboundFailure: g.deliverAsyncInboundFailure,
	}
}

func (g *LiveGateway) parseCardActionTriggerEvent(event *larkcallback.CardActionTriggerEvent) (control.Action, bool) {
	return gatewaypkg.ParseCardActionTriggerEvent(g.routingEnv(), event)
}

func (g *LiveGateway) parseMessageEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) (control.Action, bool, error) {
	return gatewaypkg.ParseMessageEvent(ctx, g.inboundEnv(), event)
}

func (g *LiveGateway) parseMessageRecalledEvent(event *larkim.P2MessageRecalledV1) (control.Action, bool) {
	return gatewaypkg.ParseMessageRecalledEvent(g.inboundEnv(), event)
}

func (g *LiveGateway) parseMessageReactionCreatedEvent(event *larkim.P2MessageReactionCreatedV1) (control.Action, bool) {
	return gatewaypkg.ParseMessageReactionCreatedEvent(g.inboundEnv(), event)
}

func (g *LiveGateway) parseMenuEvent(event *larkapplication.P2BotMenuV6) (control.Action, bool) {
	return gatewaypkg.ParseMenuEvent(g.config.GatewayID, event)
}

func (g *LiveGateway) handleInboundMessageEvent(ctx context.Context, event *larkim.P2MessageReceiveV1, handler ActionHandler, lane *surfaceInboundLane) error {
	return gatewaypkg.HandleInboundMessageEvent(ctx, g.inboundEnv(), event, surfaceLaneInner(lane), gatewayDispatcher(handler))
}

func (g *LiveGateway) handleInboundMessageRecalledEvent(ctx context.Context, event *larkim.P2MessageRecalledV1, handler ActionHandler, lane *surfaceInboundLane) error {
	action, ok := g.parseMessageRecalledEvent(event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: action}) {
		return nil
	}
	return handleGatewayEventAction(ctx, action, handler)
}

func (g *LiveGateway) handleInboundMessageReactionCreatedEvent(ctx context.Context, event *larkim.P2MessageReactionCreatedV1, handler ActionHandler, lane *surfaceInboundLane) error {
	action, ok := g.parseMessageReactionCreatedEvent(event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: action}) {
		return nil
	}
	return handleGatewayEventAction(ctx, action, handler)
}

func (g *LiveGateway) planInboundMessageEvent(event *larkim.P2MessageReceiveV1) (plannedInboundMessage, bool, error) {
	plan, ok, err := gatewaypkg.PlanInboundMessageEvent(g.inboundEnv(), event)
	if err != nil || !ok {
		return plannedInboundMessage{}, ok, err
	}
	out := plannedInboundMessage{action: plan.Action}
	if plan.Queue != nil {
		out.queue = &queuedMessageWork{inner: plan.Queue}
	}
	return out, true, nil
}

func (g *LiveGateway) lookupSurfaceMessage(messageID string) string {
	if g == nil {
		return ""
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.TrimSpace(g.messages[messageID])
}

func (g *LiveGateway) surfaceForCardAction(messageID, chatID, operatorID string) string {
	if g == nil {
		return ""
	}
	if surfaceID := g.lookupSurfaceMessage(messageID); surfaceID != "" {
		return surfaceID
	}
	return surfaceIDForInbound(g.config.GatewayID, chatID, "", operatorID)
}

func (g *LiveGateway) deliverAsyncInboundFailure(ctx context.Context, surfaceID, chatID, actorUserID, replyToMessageID, body string) {
	if g == nil || strings.TrimSpace(body) == "" {
		return
	}
	receiveID, receiveIDType := ResolveReceiveTarget(chatID, actorUserID)
	if receiveID == "" || receiveIDType == "" {
		return
	}
	op := Operation{
		Kind:             OperationSendCard,
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		ReceiveID:        receiveID,
		ReceiveIDType:    receiveIDType,
		ChatID:           strings.TrimSpace(chatID),
		ReplyToMessageID: strings.TrimSpace(replyToMessageID),
		CardTitle:        "消息未处理",
		CardBody:         body,
		CardThemeKey:     cardThemeError,
		cardEnvelope:     cardEnvelopeV2,
		card:             legacyCardDocument("消息未处理", body, cardThemeError, nil),
	}
	applyCtx, cancel := newFeishuTimeoutContext(ctx, asyncInboundFailureNoticeTimeout)
	defer cancel()
	if err := g.Apply(applyCtx, []Operation{op}); err != nil {
		log.Printf("feishu async inbound failure notice delivery failed: surface=%s reply_to=%s err=%v", surfaceID, replyToMessageID, err)
	}
}

func surfaceLaneInner(lane *surfaceInboundLane) *gatewaypkg.SurfaceInboundLane {
	if lane == nil {
		return nil
	}
	return lane.inner
}

func parseTextAction(text string) (control.Action, bool) {
	return control.ParseFeishuTextAction(text)
}

func menuAction(eventKey string) (control.Action, bool) {
	return control.ParseFeishuMenuAction(eventKey)
}

func normalizeMenuEventKey(value string) string {
	return control.NormalizeFeishuMenuEventKey(value)
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func parseTextContent(rawContent string) (string, error) {
	return gatewaypkg.ParseTextContent(rawContent)
}

func parseImageKey(rawContent string) (string, error) {
	return gatewaypkg.ParseImageKey(rawContent)
}

func parseFileContent(rawContent string) (string, string, error) {
	return gatewaypkg.ParseFileContent(rawContent)
}

func parseMergeForwardContent(rawContent string) (string, error) {
	return gatewaypkg.ParseMergeForwardContent(rawContent)
}

func looksLikeJSONObject(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return true
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return true
	}
	return false
}

func firstJSONString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch current := value.(type) {
		case string:
			if text := strings.TrimSpace(current); text != "" {
				return text
			}
		}
	}
	return ""
}

func linesFromMessageIDs(payload map[string]any) []string {
	raw, ok := payload["message_id_list"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("包含 %d 条转发消息", len(items))}
}

func mergeForwardTitle(rawContent string) string {
	return gatewaypkg.MergeForwardTitle(rawContent)
}

func parseFileName(rawContent string) string {
	return gatewaypkg.ParseFileName(rawContent)
}

func gatewayMessageSpeakerLabel(message *gatewayMessage) string {
	if message == nil {
		return ""
	}
	return gatewaypkg.GatewayMessageSpeakerLabel(message.SenderID, message.SenderType)
}

func surfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID string) string {
	return gatewaypkg.SurfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID)
}

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	return gatewaypkg.ResolveReceiveTarget(chatID, actorUserID)
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

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func intMapValue(values map[string]interface{}, key string) int {
	if len(values) == 0 {
		return 0
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}
	switch current := value.(type) {
	case int:
		return current
	case int8:
		return int(current)
	case int16:
		return int(current)
	case int32:
		return int(current)
	case int64:
		return int(current)
	case uint:
		return int(current)
	case uint8:
		return int(current)
	case uint16:
		return int(current)
	case uint32:
		return int(current)
	case uint64:
		return int(current)
	case float32:
		return int(current)
	case float64:
		return int(current)
	case string:
		current = strings.TrimSpace(current)
		if current == "" {
			return 0
		}
		parsed, err := strconv.Atoi(current)
		if err == nil {
			return parsed
		}
	default:
		return 0
	}
	return 0
}
