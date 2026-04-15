package feishu

import (
	"context"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

const (
	defaultLarkRequestTimeout            = 2 * time.Minute
	sendIMFileTimeout                    = 2 * time.Minute
	inboundMessageParseTimeout           = 30 * time.Second
	asyncInboundFailureNoticeTimeout     = 10 * time.Second
	previewDriveSummaryTimeout           = 20 * time.Second
	previewDriveCleanupTimeout           = 45 * time.Second
	previewDriveBackgroundCleanupTimeout = 45 * time.Second
)

func NewLarkClient(appID, appSecret string) *lark.Client {
	return lark.NewClient(
		strings.TrimSpace(appID),
		strings.TrimSpace(appSecret),
		lark.WithReqTimeout(defaultLarkRequestTimeout),
	)
}

func newFeishuTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = parent
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}
