package feishu

import (
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

func NewLarkClient(appID, appSecret string, options ...lark.ClientOptionFunc) *lark.Client {
	clientOptions := []lark.ClientOptionFunc{
		lark.WithReqTimeout(defaultLarkRequestTimeout),
	}
	clientOptions = append(clientOptions, options...)
	return lark.NewClient(
		strings.TrimSpace(appID),
		strings.TrimSpace(appSecret),
		clientOptions...,
	)
}

func NewLarkClientWithOpenBaseURL(appID, appSecret, openBaseURL string) *lark.Client {
	var options []lark.ClientOptionFunc
	if openBaseURL = strings.TrimSpace(openBaseURL); openBaseURL != "" {
		options = append(options, lark.WithOpenBaseUrl(openBaseURL))
	}
	return NewLarkClient(appID, appSecret, options...)
}
