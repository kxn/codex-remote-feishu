package feishu

import (
	"context"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkimv2 "github.com/larksuite/oapi-sdk-go/v3/service/im/v2"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type ActionHandler func(context.Context, control.Action) *ActionResult

type ActionResult struct {
	ReplaceCurrentCard *Operation
}

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
	Domain         string
	TempDir        string
	UseSystemProxy bool
}

type LiveGateway struct {
	config LiveGatewayConfig
	client *lark.Client
	broker *FeishuCallBroker

	downloadImageFn    func(context.Context, string, string) (string, string, error)
	downloadFileFn     func(context.Context, string, string, string) (string, error)
	uploadImagePathFn  func(context.Context, string) (string, error)
	uploadImageBytesFn func(context.Context, []byte) (string, error)
	uploadFilePathFn   func(context.Context, string) (string, string, error)
	uploadVideoPathFn  func(context.Context, string) (string, string, error)
	fetchMessageFn     func(context.Context, string) (*gatewayMessage, error)
	createMessageFn    func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error)
	replyMessageFn     func(context.Context, string, string, string) (*larkim.ReplyMessageResp, error)
	patchMessageFn     func(context.Context, string, string) (*larkim.PatchMessageResp, error)
	deleteMessageFn    func(context.Context, string) (*larkim.DeleteMessageResp, error)
	createReactionFn   func(context.Context, string, string) (*larkim.CreateMessageReactionResp, error)
	deleteReactionFn   func(context.Context, string, string) (*larkim.DeleteMessageReactionResp, error)
	botTimeSensitiveFn func(context.Context, string, bool, []string) (*larkimv2.BotTimeSentiveFeedCardResp, error)

	mu        sync.Mutex
	stateHook func(GatewayState, error)
	reactions map[string]string
	messages  map[string]string
}

type gatewayMessage struct {
	MessageID      string
	MessageType    string
	Content        string
	Deleted        bool
	UpperMessageID string
	SenderID       string
	SenderType     string
	Children       []*gatewayMessage
}

type feishuTextContent struct {
	Text string `json:"text"`
}

type feishuPostContent struct {
	Title   string             `json:"title"`
	Content [][]feishuPostNode `json:"content"`
}

type feishuLocalizedPostContent struct {
	ZhCN feishuPostContent `json:"zh_cn"`
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

func NewLiveGateway(config LiveGatewayConfig) *LiveGateway {
	config.GatewayID = normalizeGatewayID(config.GatewayID)
	client := NewLarkClient(config.AppID, config.AppSecret)
	gateway := &LiveGateway{
		config:    config,
		client:    client,
		broker:    NewFeishuCallBroker(config.GatewayID, client),
		reactions: map[string]string{},
		messages:  map[string]string{},
	}
	gateway.downloadImageFn = gateway.downloadImage
	gateway.downloadFileFn = gateway.downloadFile
	gateway.uploadImagePathFn = gateway.uploadImagePath
	gateway.uploadImageBytesFn = gateway.uploadImageBytes
	gateway.uploadFilePathFn = gateway.uploadFilePath
	gateway.uploadVideoPathFn = gateway.uploadVideoPath
	gateway.fetchMessageFn = gateway.fetchMessage
	gateway.createMessageFn = gateway.createMessage
	gateway.replyMessageFn = gateway.replyMessage
	gateway.patchMessageFn = gateway.patchMessage
	gateway.deleteMessageFn = gateway.deleteMessage
	gateway.createReactionFn = gateway.createReaction
	gateway.deleteReactionFn = gateway.deleteReaction
	gateway.botTimeSensitiveFn = gateway.botTimeSensitive
	return gateway
}

func (g *LiveGateway) Client() *lark.Client {
	if g == nil {
		return nil
	}
	return g.client
}

func (g *LiveGateway) ClearGrantedPermissionBlocks(scopes []AppScopeStatus) {
	if g == nil || g.broker == nil {
		return
	}
	g.broker.ClearGrantedPermissionBlocks(scopes)
}
