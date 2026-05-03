package gateway

import (
	"context"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type RoutingEnv struct {
	GatewayID            string
	SurfaceForCardAction func(messageID, chatID, operatorID string) string
}

type InboundEnv struct {
	GatewayID                        string
	LookupSurfaceMessage             func(messageID string) string
	ParseTextActionWithoutCatalog    func(text string) (control.Action, bool)
	QuotedInputs                     func(context.Context, *larkim.EventMessage) []agentproto.Input
	ParsePostInputs                  func(context.Context, string, string) ([]agentproto.Input, string, error)
	BuildMergeForwardStructuredInput func(context.Context, *larkim.EventMessage) (string, []agentproto.Input, error)
	RecordSurfaceMessage             func(messageID, surfaceSessionID string)
	DownloadImage                    func(context.Context, string, string) (string, string, error)
	DownloadFile                     func(context.Context, string, string, string) (string, error)
	DeliverAsyncInboundFailure       func(context.Context, string, string, string, string, string)
}

type ActionDispatcher func(context.Context, control.Action) error
