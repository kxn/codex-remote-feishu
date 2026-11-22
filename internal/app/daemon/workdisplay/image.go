package workdisplay

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (p *projection) image() error {
	if execFailed(p.event.Status) {
		return p.notice("图像生成失败", "本轮未返回可发送的图片。")
	}
	data := imagePayload(xutil.MetadataString(p.event.Metadata, "imageBase64"))
	name := xutil.FirstNonEmpty(xutil.MetadataString(p.event.Metadata, "imageFileName"), "generated-image")
	mimeType := http.DetectContentType(data)
	if len(data) == 0 || len(data) > feishu.MaxPrivateImageBytes || !strings.HasPrefix(mimeType, "image/") {
		return p.notice("图片暂未发送", "原生执行返回的图片无法读取或超过 10 MB。可在当前工作目录内另存图片后重新发送。")
	}
	v := p.display
	content := work.NewContent(stableID("image", v.ID, p.event.ItemID), v.Binding.ScopeID, mimeType, data, p.now)
	if old, err := p.tx.GetContent(v.Binding.ScopeID, content.ID); err == nil {
		if old.SHA256 == content.SHA256 {
			return nil
		}
		return p.notice("图片暂未发送", "同一原生图片的内容发生变化。可将需要的版本另存后发送。")
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	if err := p.tx.PutContent(content); err != nil {
		return err
	}
	_, err := p.enqueue(nil, &workservice.PrivateMediaPublication{Kind: "image", FileName: name, ContentRef: content.ID, SHA256: content.SHA256}, "", "", "")
	if err == nil {
		v.HadOutput = true
	}
	return err
}

func imagePayload(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "data:image/") {
		_, raw, _ = strings.Cut(raw, ",")
	}
	if len(raw) > base64.StdEncoding.EncodedLen(feishu.MaxPrivateImageBytes) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	return data
}

func execFailed(status string) bool {
	return status == "failed" || status == "interrupted"
}
