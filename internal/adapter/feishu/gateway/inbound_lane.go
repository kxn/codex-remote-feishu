package gateway

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
	run(context.Context, InboundEnv, ActionDispatcher)
}

type SurfaceInboundLane struct {
	ctx      context.Context
	env      InboundEnv
	dispatch ActionDispatcher

	mu      sync.Mutex
	queues  map[string][]inboundWork
	running map[string]bool
	dedupe  map[string]time.Time
}

type QueuedMessageWork struct {
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

type PlannedInboundMessage struct {
	Action *control.Action
	Queue  *QueuedMessageWork
}

func NewSurfaceInboundLane(ctx context.Context, env InboundEnv, dispatch ActionDispatcher) *SurfaceInboundLane {
	return &SurfaceInboundLane{
		ctx:      ctx,
		env:      env,
		dispatch: dispatch,
		queues:   map[string][]inboundWork{},
		running:  map[string]bool{},
		dedupe:   map[string]time.Time{},
	}
}

func (l *SurfaceInboundLane) enqueue(work inboundWork) bool {
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

func (l *SurfaceInboundLane) markActionDuplicate(action control.Action) bool {
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

func (l *SurfaceInboundLane) dedupeKeyLocked(surfaceID, key, description string, now time.Time) bool {
	if expiresAt, ok := l.dedupe[key]; ok && expiresAt.After(now) {
		log.Printf("feishu inbound duplicate suppressed: surface=%s key=%s work=%s", surfaceID, key, description)
		return true
	}
	l.dedupe[key] = now.Add(inboundEventDedupeWindow)
	return false
}

func (l *SurfaceInboundLane) runSurface(surfaceID string) {
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
			work.run(l.ctx, l.env, l.dispatch)
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

func (l *SurfaceInboundLane) dequeue(surfaceID string) inboundWork {
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

func (l *SurfaceInboundLane) clearSurface(surfaceID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.queues, surfaceID)
	delete(l.running, surfaceID)
}

func (l *SurfaceInboundLane) pruneExpiredDedupeLocked(now time.Time) {
	for key, expiresAt := range l.dedupe {
		if !expiresAt.After(now) {
			delete(l.dedupe, key)
		}
	}
}

func (w *QueuedMessageWork) surfaceSessionID() string {
	if w == nil {
		return ""
	}
	return strings.TrimSpace(w.surfaceID)
}

func (w *QueuedMessageWork) dedupeKey() string {
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

func (w *QueuedMessageWork) description() string {
	if w == nil {
		return "message"
	}
	return "message:" + strings.TrimSpace(w.messageType)
}

func (w *QueuedMessageWork) run(ctx context.Context, env InboundEnv, dispatch ActionDispatcher) {
	if w == nil || dispatch == nil {
		return
	}
	parseCtx, cancel := newFeishuTimeoutContext(ctx, inboundMessageParseTimeout)
	defer cancel()

	action, ok, err := w.parseAction(parseCtx, env)
	if err != nil {
		msg := w.eventMessage()
		logInboundMessageParseFailed(w.gatewayID, w.surfaceID, w.inbound, msg, "async_parse", err)
		if env.DeliverAsyncInboundFailure != nil {
			env.DeliverAsyncInboundFailure(ctx, w.surfaceID, w.chatID, w.actorUserID, w.messageID, asyncInboundFailureNoticeBody(w.messageType))
		}
		return
	}
	if !ok {
		msg := w.eventMessage()
		logInboundMessageIgnored(w.gatewayID, w.surfaceID, w.inbound, msg, "async_empty_or_unsupported")
		return
	}
	_ = dispatch(ctx, action)
}

func (w *QueuedMessageWork) parseAction(ctx context.Context, env InboundEnv) (control.Action, bool, error) {
	if w == nil {
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
		action.Inputs = append(env.QuotedInputs(ctx, message), currentInputs...)
		action.SteerInputs = currentInputs
		return action, true, nil
	case "post":
		inputs, text, err := env.ParsePostInputs(ctx, w.messageID, w.content)
		if err != nil {
			return control.Action{}, false, err
		}
		if len(inputs) == 0 {
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = text
		action.Inputs = append(env.QuotedInputs(ctx, message), inputs...)
		action.SteerInputs = append([]agentproto.Input(nil), inputs...)
		return action, true, nil
	case "image":
		path, mimeType, err := env.DownloadImage(ctx, w.messageID, w.imageKey)
		if err != nil {
			return control.Action{}, false, err
		}
		action.Kind = control.ActionImageMessage
		action.LocalPath = path
		action.MIMEType = mimeType
		action.SteerInputs = []agentproto.Input{{Type: agentproto.InputLocalImage, Path: path, MIMEType: mimeType}}
		return action, true, nil
	case "file":
		path, err := env.DownloadFile(ctx, w.messageID, w.fileKey, w.fileName)
		if err != nil {
			return control.Action{}, false, err
		}
		action.Kind = control.ActionFileMessage
		action.LocalPath = path
		action.FileName = w.fileName
		return action, true, nil
	case "merge_forward":
		summary, inputs, err := env.BuildMergeForwardStructuredInput(ctx, message)
		if err != nil {
			return control.Action{}, false, err
		}
		if len(inputs) == 0 {
			return control.Action{}, false, nil
		}
		action.Kind = control.ActionTextMessage
		action.Text = summary
		action.Inputs = append(env.QuotedInputs(ctx, message), inputs...)
		return action, true, nil
	default:
		return control.Action{}, false, nil
	}
}

func (w *QueuedMessageWork) eventMessage() *larkim.EventMessage {
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

func (w *queuedActionWork) run(ctx context.Context, _ InboundEnv, dispatch ActionDispatcher) {
	if dispatch == nil {
		return
	}
	_ = dispatch(ctx, cloneAction(w.action))
}

func (l *SurfaceInboundLane) EnqueueQueuedMessage(work *QueuedMessageWork) bool {
	return l.enqueue(work)
}

func (l *SurfaceInboundLane) EnqueueAction(action control.Action) bool {
	return l.enqueue(&queuedActionWork{action: cloneAction(action)})
}

func (l *SurfaceInboundLane) MarkActionDuplicate(action control.Action) bool {
	return l.markActionDuplicate(action)
}

func HandleInboundMessageEvent(ctx context.Context, env InboundEnv, event *larkim.P2MessageReceiveV1, lane *SurfaceInboundLane, dispatch ActionDispatcher) error {
	plan, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil || !ok {
		return err
	}
	if plan.Action != nil {
		if lane != nil && lane.markActionDuplicate(*plan.Action) {
			return nil
		}
		return dispatch(ctx, *plan.Action)
	}
	if plan.Queue == nil {
		return nil
	}
	if lane != nil && lane.enqueue(plan.Queue) {
		return nil
	}
	action, ok, err := plan.Queue.parseAction(ctx, env)
	if err != nil || !ok {
		return err
	}
	return dispatch(ctx, action)
}

func HandleInboundMessageRecalledEvent(ctx context.Context, env InboundEnv, event *larkim.P2MessageRecalledV1, lane *SurfaceInboundLane, dispatch ActionDispatcher) error {
	action, ok := ParseMessageRecalledEvent(env, event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: cloneAction(action)}) {
		return nil
	}
	return dispatch(ctx, action)
}

func HandleInboundMessageReactionCreatedEvent(ctx context.Context, env InboundEnv, event *larkim.P2MessageReactionCreatedV1, lane *SurfaceInboundLane, dispatch ActionDispatcher) error {
	action, ok := ParseMessageReactionCreatedEvent(env, event)
	if !ok {
		return nil
	}
	if lane != nil && lane.enqueue(&queuedActionWork{action: cloneAction(action)}) {
		return nil
	}
	return dispatch(ctx, action)
}

func PlanInboundMessageEvent(env InboundEnv, event *larkim.P2MessageReceiveV1) (PlannedInboundMessage, bool, error) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return PlannedInboundMessage{}, false, nil
	}
	message := event.Event.Message
	chatID := stringPtr(message.ChatId)
	chatType := stringPtr(message.ChatType)
	senderUserID := userIDFromMessage(event.Event.Sender)
	gatewayID := strings.TrimSpace(env.GatewayID)
	surfaceSessionID := SurfaceIDForInbound(gatewayID, chatID, chatType, senderUserID)
	inbound := InboundMetaFromMessageEvent(event)
	messageID := strings.TrimSpace(stringPtr(message.MessageId))
	messageType := strings.ToLower(strings.TrimSpace(stringPtr(message.MessageType)))
	content := stringPtr(message.Content)

	baseAction := control.Action{
		GatewayID:        gatewayID,
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
		text, commandText, err := parseFeishuEventText(content, message.Mentions)
		if err != nil {
			logInboundMessageParseFailed(gatewayID, surfaceSessionID, inbound, message, "parse_text_content", err)
			return PlannedInboundMessage{}, false, err
		}
		commandAction, handled := env.ParseTextAction(commandText)
		if handled {
			commandAction.GatewayID = gatewayID
			commandAction.SurfaceSessionID = surfaceSessionID
			commandAction.ChatID = chatID
			commandAction.ActorUserID = baseAction.ActorUserID
			commandAction.MessageID = baseAction.MessageID
			commandAction.TargetMessageID = baseAction.TargetMessageID
			commandAction.Inbound = cloneInboundMeta(inbound)
			return PlannedInboundMessage{Action: &commandAction}, true, nil
		}
		env.RecordSurfaceMessage(messageID, surfaceSessionID)
		return PlannedInboundMessage{
			Queue: &QueuedMessageWork{
				gatewayID:       gatewayID,
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
			logInboundMessageParseFailed(gatewayID, surfaceSessionID, inbound, message, "parse_post_content", err)
			return PlannedInboundMessage{}, false, err
		}
		env.RecordSurfaceMessage(messageID, surfaceSessionID)
		return PlannedInboundMessage{
			Queue: &QueuedMessageWork{
				gatewayID:       gatewayID,
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
		imageKey, err := ParseImageKey(content)
		if err != nil {
			logInboundMessageParseFailed(gatewayID, surfaceSessionID, inbound, message, "parse_image_content", err)
			return PlannedInboundMessage{}, false, err
		}
		env.RecordSurfaceMessage(messageID, surfaceSessionID)
		return PlannedInboundMessage{
			Queue: &QueuedMessageWork{
				gatewayID:       gatewayID,
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
		fileKey, fileName, err := ParseFileContent(content)
		if err != nil {
			logInboundMessageParseFailed(gatewayID, surfaceSessionID, inbound, message, "parse_file_content", err)
			return PlannedInboundMessage{}, false, err
		}
		env.RecordSurfaceMessage(messageID, surfaceSessionID)
		return PlannedInboundMessage{
			Queue: &QueuedMessageWork{
				gatewayID:       gatewayID,
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
		env.RecordSurfaceMessage(messageID, surfaceSessionID)
		return PlannedInboundMessage{
			Queue: &QueuedMessageWork{
				gatewayID:       gatewayID,
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
		logInboundMessageIgnored(gatewayID, surfaceSessionID, inbound, message, "unsupported_message_type")
		return PlannedInboundMessage{}, false, nil
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
