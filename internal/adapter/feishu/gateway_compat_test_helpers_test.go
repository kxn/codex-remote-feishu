package feishu

import (
	"context"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

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

type inboundWork interface {
	enqueue(*surfaceInboundLane) bool
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
	return gatewaypkg.HandleInboundMessageRecalledEvent(ctx, g.inboundEnv(), event, surfaceLaneInner(lane), gatewayDispatcher(handler))
}

func (g *LiveGateway) handleInboundMessageReactionCreatedEvent(ctx context.Context, event *larkim.P2MessageReactionCreatedV1, handler ActionHandler, lane *surfaceInboundLane) error {
	return gatewaypkg.HandleInboundMessageReactionCreatedEvent(ctx, g.inboundEnv(), event, surfaceLaneInner(lane), gatewayDispatcher(handler))
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

func surfaceLaneInner(lane *surfaceInboundLane) *gatewaypkg.SurfaceInboundLane {
	if lane == nil {
		return nil
	}
	return lane.inner
}

func menuAction(eventKey string) (control.Action, bool) {
	return control.ParseFeishuMenuActionWithoutCatalog(eventKey)
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func normalizeMenuEventKey(value string) string {
	return control.NormalizeFeishuMenuEventKey(value)
}

func parseTextAction(text string) (control.Action, bool) {
	return control.ParseFeishuTextActionWithoutCatalog(text)
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
