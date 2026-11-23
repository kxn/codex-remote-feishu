package daemon

import (
	"context"
	"encoding/base64"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var steerImageBytes, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jf1kAAAAASUVORK5CYII=")

type steerMediaExpander struct{}

func (steerMediaExpander) Expand(ctx context.Context, raw work.RawIngressEnvelope) (workingress.ExpandedMessage, error) {
	if raw.MessageType != "image" {
		return (workingress.DefaultExpander{}).Expand(ctx, raw)
	}
	return workingress.ExpandedMessage{Text: "[已读取图片附件]", Complete: true, Attachments: []workingress.ExpandedAttachment{{Kind: "image", MediaType: "image/png", SourceMessageID: raw.MessageID, Data: steerImageBytes}}}, nil
}
