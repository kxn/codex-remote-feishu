package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const inboundEventDedupeWindow = 10 * time.Minute

type inboundWork interface {
	surfaceSessionID() string
	dedupeKey() string
	description() string
	run(context.Context, *LiveGateway, ActionHandler)
}

type surfaceInboundLane struct {
	ctx     context.Context
	gateway *LiveGateway
	handler ActionHandler

	mu      sync.Mutex
	queues  map[string][]inboundWork
	running map[string]bool
	dedupe  map[string]time.Time
}

type queuedMessageWork struct {
	gatewayID       string
	surfaceID       string
	chatID          string
	actorUserID     string
	messageID       string
	messageType     string
	content         string
	parentMessageID string
	rootMessageID   string
	inbound         *control.ActionInboundMeta
	text            string
	imageKey        string
	fileKey         string
	fileName        string
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
		ctx:     ctx,
		gateway: gateway,
		handler: handler,
		queues:  map[string][]inboundWork{},
		running: map[string]bool{},
		dedupe:  map[string]time.Time{},
	}
}

func (l *surfaceInboundLane) enqueue(work inboundWork) bool {
	if l == nil || work == nil {
		return false
	}
	if err := l.ctx.Err(); err != nil {
		return false
	}
	surfaceID := strings.TrimSpace(work.surfaceSessionID())
	if surfaceID == "" {
		return false
	}
	now := time.Now()
	key := strings.TrimSpace(work.dedupeKey())

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneExpiredDedupeLocked(now)
	if key != "" {
		if l.dedupeKeyLocked(surfaceID, key, work.description(), now) {
			return true
		}
	}
	l.queues[surfaceID] = append(l.queues[surfaceID], work)
	if l.running[surfaceID] {
		return true
	}
	l.running[surfaceID] = true
	go l.runSurface(surfaceID)
	return true
}

func (l *surfaceInboundLane) markActionDuplicate(action control.Action) bool {
	if l == nil {
		return false
	}
	if err := l.ctx.Err(); err != nil {
		return false
	}
	surfaceID := strings.TrimSpace(action.SurfaceSessionID)
	if surfaceID == "" {
		return false
	}
	key := dedupeKeyForAction(action)
	if key == "" {
		return false
	}
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneExpiredDedupeLocked(now)
	return l.dedupeKeyLocked(surfaceID, key, "action:"+string(action.Kind), now)
}

func (l *surfaceInboundLane) dedupeKeyLocked(surfaceID, key, description string, now time.Time) bool {
	if expiresAt, ok := l.dedupe[key]; ok && expiresAt.After(now) {
		log.Printf("feishu inbound duplicate suppressed: surface=%s key=%s work=%s", surfaceID, key, description)
		return true
	}
	l.dedupe[key] = now.Add(inboundEventDedupeWindow)
	return false
}

func (l *surfaceInboundLane) runSurface(surfaceID string) {
	for {
		if err := l.ctx.Err(); err != nil {
			l.clearSurface(surfaceID)
			return
		}
		work := l.dequeue(surfaceID)
		if work == nil {
			return
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("feishu inbound worker panic: surface=%s work=%s panic=%v", surfaceID, work.description(), recovered)
				}
			}()
			work.run(l.ctx, l.gateway, l.handler)
		}()
	}
}

func dedupeKeyForAction(action control.Action) string {
	if action.Inbound == nil {
		return ""
	}
	if key := strings.TrimSpace(action.Inbound.EventID); key != "" {
		return "event:" + key
	}
	if key := strings.TrimSpace(action.Inbound.RequestID); key != "" {
		return "request:" + key
	}
	return ""
}

func (l *surfaceInboundLane) dequeue(surfaceID string) inboundWork {
	l.mu.Lock()
	defer l.mu.Unlock()

	queue := l.queues[surfaceID]
	if len(queue) == 0 {
		delete(l.queues, surfaceID)
		delete(l.running, surfaceID)
		return nil
	}
	work := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		delete(l.queues, surfaceID)
	} else {
		l.queues[surfaceID] = queue
	}
	return work
}

func (l *surfaceInboundLane) clearSurface(surfaceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.queues, surfaceID)
	delete(l.running, surfaceID)
}

func (l *surfaceInboundLane) pruneExpiredDedupeLocked(now time.Time) {
	for key, expiresAt := range l.dedupe {
		if !expiresAt.After(now) {
			delete(l.dedupe, key)
		}
	}
}

func (w *queuedMessageWork) surfaceSessionID() string {
	if w == nil {
		return ""
	}
	return strings.TrimSpace(w.surfaceID)
}

func (w *queuedMessageWork) dedupeKey() string {
	if w == nil || w.inbound == nil {
		return ""
	}
	if key := strings.TrimSpace(w.inbound.EventID); key != "" {
		return "event:" + key
	}
	if key := strings.TrimSpace(w.inbound.RequestID); key != "" {
		return "request:" + key
	}
	return ""
}

func (w *queuedMessageWork) description() string {
	if w == nil {
		return "message"
	}
	return "message:" + strings.TrimSpace(w.messageType)
}

func (w *queuedMessageWork) run(ctx context.Context, gateway *LiveGateway, handler ActionHandler) {
	if w == nil || gateway == nil || handler == nil {
		return
	}
	parseCtx, cancel := newFeishuTimeoutContext(ctx, inboundMessageParseTimeout)
	defer cancel()

	action, ok, err := w.parseAction(parseCtx, gateway)
	if err != nil {
		msg := w.eventMessage()
		logInboundMessageParseFailed(w.gatewayID, w.surfaceID, w.inbound, msg, "async_parse", err)
		gateway.deliverAsyncInboundFailure(ctx, w.surfaceID, w.chatID, w.actorUserID, w.messageID, asyncInboundFailureNoticeBody(w.messageType))
		return
	}
	if !ok {
		msg := w.eventMessage()
		logInboundMessageIgnored(w.gatewayID, w.surfaceID, w.inbound, msg, "async_empty_or_unsupported")
		return
	}
	handler(ctx, action)
}

func (w *queuedMessageWork) parseAction(ctx context.Context, gateway *LiveGateway) (control.Action, bool, error) {
	if w == nil || gateway == nil {
		return control.Action{}, false, nil
	}
	message := w.eventMessage()
	action := control.Action{
		GatewayID:        w.gatewayID,
		SurfaceSessionID: w.surfaceID,
		ChatID:           w.chatID,
		ActorUserID:      w.actorUserID,
		MessageID:        w.messageID,
		Inbound:          cloneInboundMeta(w.inbound),
	}
	replyTargetMessageID := referencedMessageID(message)
	if replyTargetMessageID != "" {
		action.TargetMessageID = replyTargetMessageID
	}

	switch strings.ToLower(strings.TrimSpace(w.messageType)) {
	case "text":
		currentInputs := []agentproto.Input{{Type: agentproto.InputText, Text: w.text}}
		action.Kind = control.ActionTextMessage
		action.Text = w.text
		action.Inputs = append(gateway.quotedInputs(ctx, message), currentInputs...)
		action.SteerInputs = currentInputs
		return action, true, nil
	case "post":
		inputs, text, err := gateway.parsePostInputs(ctx, w.messageID, w.content)
		if err != nil {
			return control.Action{}, false, err
		}
		if len(inputs) == 0 {
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = text
		action.Inputs = append(gateway.quotedInputs(ctx, message), inputs...)
		action.SteerInputs = append([]agentproto.Input(nil), inputs...)
		return action, true, nil
	case "image":
		path, mimeType, err := gateway.downloadImageFn(ctx, w.messageID, w.imageKey)
		if err != nil {
			return control.Action{}, false, err
		}
		action.Kind = control.ActionImageMessage
		action.LocalPath = path
		action.MIMEType = mimeType
		action.SteerInputs = []agentproto.Input{{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType}}
		return action, true, nil
	case "file":
		path, err := gateway.downloadFileFn(ctx, w.messageID, w.fileKey, w.fileName)
		if err != nil {
			return control.Action{}, false, err
		}
		action.Kind = control.ActionFileMessage
		action.LocalPath = path
		action.FileName = w.fileName
		return action, true, nil
	case "merge_forward":
		payload, err := gateway.buildMergeForwardStructuredPayloadFromEvent(ctx, message)
		if err != nil {
			return control.Action{}, false, err
		}
		if len(payload.Inputs) == 0 {
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = payload.Summary
		action.Inputs = append(gateway.quotedInputs(ctx, message), payload.Inputs...)
		return action, true, nil
	default:
		return control.Action{}, false, nil
	}
}

func (w *queuedMessageWork) eventMessage() *larkim.EventMessage {
	if w == nil {
		return nil
	}
	message := &larkim.EventMessage{}
	if w.messageID != "" {
		message.MessageId = stringValueRef(w.messageID)
	}
	if w.messageType != "" {
		message.MessageType = stringValueRef(w.messageType)
	}
	if w.content != "" {
		message.Content = stringValueRef(w.content)
	}
	if w.parentMessageID != "" {
		message.ParentId = stringValueRef(w.parentMessageID)
	}
	if w.rootMessageID != "" {
		message.RootId = stringValueRef(w.rootMessageID)
	}
	if w.chatID != "" {
		message.ChatId = stringValueRef(w.chatID)
	}
	if w.inbound != nil && !w.inbound.MessageCreateTime.IsZero() {
		message.CreateTime = stringValueRef(fmt.Sprintf("%d", w.inbound.MessageCreateTime.UnixMilli()))
	}
	return message
}

func (w *queuedActionWork) surfaceSessionID() string {
	return strings.TrimSpace(w.action.SurfaceSessionID)
}

func (w *queuedActionWork) dedupeKey() string {
	return dedupeKeyForAction(w.action)
}

func (w *queuedActionWork) description() string {
	return "action:" + string(w.action.Kind)
}

func (w *queuedActionWork) run(ctx context.Context, _ *LiveGateway, handler ActionHandler) {
	if handler == nil {
		return
	}
	handler(ctx, cloneAction(w.action))
}

func (g *LiveGateway) handleInboundMessageEvent(ctx context.Context, event *larkim.P2MessageReceiveV1, handler ActionHandler, lane *surfaceInboundLane) error {
	plan, ok, err := g.planInboundMessageEvent(event)
	if err != nil || !ok {
		return err
	}
	if plan.action != nil {
		if lane != nil && lane.markActionDuplicate(*plan.action) {
			return nil
		}
		return handleGatewayEventAction(ctx, *plan.action, handler)
	}
	if plan.queue == nil {
		return nil
	}
	if lane != nil && lane.enqueue(plan.queue) {
		return nil
	}
	action, ok, err := plan.queue.parseAction(ctx, g)
	if err != nil || !ok {
		return err
	}
	return handleGatewayEventAction(ctx, action, handler)
}

func (g *LiveGateway) handleInboundMessageRecalledEvent(ctx context.Context, event *larkim.P2MessageRecalledV1, handler ActionHandler, lane *surfaceInboundLane) error {
	action, ok := g.parseMessageRecalledEvent(event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: cloneAction(action)}) {
		return nil
	}
	return handleGatewayEventAction(ctx, action, handler)
}

func (g *LiveGateway) handleInboundMessageReactionCreatedEvent(ctx context.Context, event *larkim.P2MessageReactionCreatedV1, handler ActionHandler, lane *surfaceInboundLane) error {
	action, ok := g.parseMessageReactionCreatedEvent(event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: cloneAction(action)}) {
		return nil
	}
	return handleGatewayEventAction(ctx, action, handler)
}

func (g *LiveGateway) planInboundMessageEvent(event *larkim.P2MessageReceiveV1) (plannedInboundMessage, bool, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return plannedInboundMessage{}, false, nil
	}
	message := event.Event.Message
	chatID := stringPtr(message.ChatId)
	chatType := stringPtr(message.ChatType)
	senderUserID := userIDFromMessage(event.Event.Sender)
	surfaceSessionID := surfaceIDForInbound(g.config.GatewayID, chatID, chatType, senderUserID)
	inbound := inboundMetaFromMessageEvent(event)
	messageID := strings.TrimSpace(stringPtr(message.MessageId))
	messageType := strings.ToLower(strings.TrimSpace(stringPtr(message.MessageType)))
	content := stringPtr(message.Content)

	baseAction := control.Action{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: surfaceSessionID,
		ChatID:           chatID,
		ActorUserID:      senderUserID,
		MessageID:        messageID,
		Inbound:          cloneInboundMeta(inbound),
	}
	if replyTargetMessageID := referencedMessageID(message); replyTargetMessageID != "" {
		baseAction.TargetMessageID = replyTargetMessageID
	}

	switch messageType {
	case "text":
		text, err := parseTextContent(content)
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, inbound, message, "parse_text_content", err)
			return plannedInboundMessage{}, false, err
		}
		commandAction, handled := parseTextAction(text)
		if handled {
			commandAction.GatewayID = g.config.GatewayID
			commandAction.SurfaceSessionID = surfaceSessionID
			commandAction.ChatID = chatID
			commandAction.ActorUserID = baseAction.ActorUserID
			commandAction.MessageID = baseAction.MessageID
			commandAction.TargetMessageID = baseAction.TargetMessageID
			commandAction.Inbound = cloneInboundMeta(inbound)
			return plannedInboundMessage{action: &commandAction}, true, nil
		}
		if fallbackAction, ok := fallbackCompatTextAction(text); ok {
			fallbackAction.GatewayID = g.config.GatewayID
			fallbackAction.SurfaceSessionID = surfaceSessionID
			fallbackAction.ChatID = chatID
			fallbackAction.ActorUserID = baseAction.ActorUserID
			fallbackAction.MessageID = baseAction.MessageID
			fallbackAction.TargetMessageID = baseAction.TargetMessageID
			fallbackAction.Inbound = cloneInboundMeta(inbound)
			return plannedInboundMessage{action: &fallbackAction}, true, nil
		}
		g.recordSurfaceMessage(messageID, surfaceSessionID)
		return plannedInboundMessage{
			queue: &queuedMessageWork{
				gatewayID:       g.config.GatewayID,
				surfaceID:       surfaceSessionID,
				chatID:          chatID,
				actorUserID:     senderUserID,
				messageID:       messageID,
				messageType:     messageType,
				content:         content,
				parentMessageID: strings.TrimSpace(stringPtr(message.ParentId)),
				rootMessageID:   strings.TrimSpace(stringPtr(message.RootId)),
				inbound:         cloneInboundMeta(inbound),
				text:            text,
			},
		}, true, nil
	case "post":
		var contentPreview feishuPostContent
		if err := json.Unmarshal([]byte(content), &contentPreview); err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, inbound, message, "parse_post_content", err)
			return plannedInboundMessage{}, false, err
		}
		g.recordSurfaceMessage(messageID, surfaceSessionID)
		return plannedInboundMessage{
			queue: &queuedMessageWork{
				gatewayID:       g.config.GatewayID,
				surfaceID:       surfaceSessionID,
				chatID:          chatID,
				actorUserID:     senderUserID,
				messageID:       messageID,
				messageType:     messageType,
				content:         content,
				parentMessageID: strings.TrimSpace(stringPtr(message.ParentId)),
				rootMessageID:   strings.TrimSpace(stringPtr(message.RootId)),
				inbound:         cloneInboundMeta(inbound),
			},
		}, true, nil
	case "image":
		imageKey, err := parseImageKey(content)
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, inbound, message, "parse_image_content", err)
			return plannedInboundMessage{}, false, err
		}
		g.recordSurfaceMessage(messageID, surfaceSessionID)
		return plannedInboundMessage{
			queue: &queuedMessageWork{
				gatewayID:       g.config.GatewayID,
				surfaceID:       surfaceSessionID,
				chatID:          chatID,
				actorUserID:     senderUserID,
				messageID:       messageID,
				messageType:     messageType,
				content:         content,
				parentMessageID: strings.TrimSpace(stringPtr(message.ParentId)),
				rootMessageID:   strings.TrimSpace(stringPtr(message.RootId)),
				inbound:         cloneInboundMeta(inbound),
				imageKey:        imageKey,
			},
		}, true, nil
	case "file":
		fileKey, fileName, err := parseFileContent(content)
		if err != nil {
			logInboundMessageParseFailed(g.config.GatewayID, surfaceSessionID, inbound, message, "parse_file_content", err)
			return plannedInboundMessage{}, false, err
		}
		g.recordSurfaceMessage(messageID, surfaceSessionID)
		return plannedInboundMessage{
			queue: &queuedMessageWork{
				gatewayID:       g.config.GatewayID,
				surfaceID:       surfaceSessionID,
				chatID:          chatID,
				actorUserID:     senderUserID,
				messageID:       messageID,
				messageType:     messageType,
				content:         content,
				parentMessageID: strings.TrimSpace(stringPtr(message.ParentId)),
				rootMessageID:   strings.TrimSpace(stringPtr(message.RootId)),
				inbound:         cloneInboundMeta(inbound),
				fileKey:         fileKey,
				fileName:        fileName,
			},
		}, true, nil
	case "merge_forward":
		g.recordSurfaceMessage(messageID, surfaceSessionID)
		return plannedInboundMessage{
			queue: &queuedMessageWork{
				gatewayID:       g.config.GatewayID,
				surfaceID:       surfaceSessionID,
				chatID:          chatID,
				actorUserID:     senderUserID,
				messageID:       messageID,
				messageType:     messageType,
				content:         content,
				parentMessageID: strings.TrimSpace(stringPtr(message.ParentId)),
				rootMessageID:   strings.TrimSpace(stringPtr(message.RootId)),
				inbound:         cloneInboundMeta(inbound),
			},
		}, true, nil
	default:
		logInboundMessageIgnored(g.config.GatewayID, surfaceSessionID, inbound, message, "unsupported_message_type")
		return plannedInboundMessage{}, false, nil
	}
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

func asyncInboundFailureNoticeBody(messageType string) string {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "merge_forward":
		return "这条转发聊天记录已经收到，但后台展开内容时失败了，暂时没有继续转交给 Codex。\n\n请稍后重试，或先缩小转发范围后再发送。"
	case "post":
		return "这条图文消息已经收到，但后台读取其中的内容或图片时失败了，暂时没有继续转交给 Codex。\n\n请稍后重试，或先简化内容后再发送。"
	case "image":
		return "这张图片已经收到，但后台读取图片时失败了，暂时没有继续转交给 Codex。\n\n请稍后重试。"
	case "file":
		return "这个文件已经收到，但后台读取文件时失败了，暂时没有继续转交给 Codex。\n\n请稍后重试。"
	default:
		return "这条消息已经收到，但后台处理引用内容或附件时失败了，暂时没有继续转交给 Codex。\n\n请稍后重试。"
	}
}

func cloneInboundMeta(meta *control.ActionInboundMeta) *control.ActionInboundMeta {
	if meta == nil {
		return nil
	}
	cloned := *meta
	return &cloned
}

func cloneAction(action control.Action) control.Action {
	cloned := action
	cloned.Inbound = cloneInboundMeta(action.Inbound)
	if len(action.Inputs) != 0 {
		cloned.Inputs = append([]agentproto.Input(nil), action.Inputs...)
	}
	if len(action.SteerInputs) != 0 {
		cloned.SteerInputs = append([]agentproto.Input(nil), action.SteerInputs...)
	}
	if len(action.RequestAnswers) != 0 {
		cloned.RequestAnswers = make(map[string][]string, len(action.RequestAnswers))
		for key, values := range action.RequestAnswers {
			cloned.RequestAnswers[key] = append([]string(nil), values...)
		}
	}
	return cloned
}

func stringValueRef(value string) *string {
	return &value
}
