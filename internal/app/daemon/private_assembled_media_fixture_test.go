package daemon

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// controlledSDKImageStager retains the production SDK parser while replacing
// only the platform's external image download with a local fixture.
type controlledSDKImageStager struct{ path string }

func (s controlledSDKImageStager) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, _ string) (control.Action, bool, error) {
	event := privateJourneySDKText(raw.MessageID, "")
	event.Event.Message.ChatId = privateJourneyString(raw.ChatID)
	event.Event.Message.MessageType = privateJourneyString(raw.MessageType)
	event.Event.Message.Content = privateJourneyString(raw.RawContent)
	event.Event.Sender.SenderId.OpenId = privateJourneyString(raw.SenderIDs.OpenID)
	env := gateway.InboundEnv{GatewayID: raw.GatewayRef, ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(string, string) {}, DownloadImage: func(context.Context, string, string) (string, string, error) { return s.path, "image/png", nil }}
	return gateway.ParsePlannedInboundAction(ctx, env, event)
}
