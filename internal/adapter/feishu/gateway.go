package feishu

import (
	"context"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

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
	Domain         string
	TempDir        string
	UseSystemProxy bool
}

type LiveGateway struct {
	config    LiveGatewayConfig
	client    *lark.Client
	projector *Projector

	downloadImageFn    func(context.Context, string, string) (string, string, error)
	uploadImagePathFn  func(context.Context, string) (string, error)
	uploadImageBytesFn func(context.Context, []byte) (string, error)
	fetchMessageFn     func(context.Context, string) (*gatewayMessage, error)
	createMessageFn    func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error)
	replyMessageFn     func(context.Context, string, string, string) (*larkim.ReplyMessageResp, error)

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
	client := lark.NewClient(config.AppID, config.AppSecret)
	gateway := &LiveGateway{
		config:    config,
		client:    client,
		projector: NewProjector(),
		reactions: map[string]string{},
		messages:  map[string]string{},
	}
	gateway.downloadImageFn = gateway.downloadImage
	gateway.uploadImagePathFn = gateway.uploadImagePath
	gateway.uploadImageBytesFn = gateway.uploadImageBytes
	gateway.fetchMessageFn = gateway.fetchMessage
	gateway.createMessageFn = gateway.createMessage
	gateway.replyMessageFn = gateway.replyMessage
	return gateway
}

func (g *LiveGateway) Client() *lark.Client {
	if g == nil {
		return nil
	}
	return g.client
}
