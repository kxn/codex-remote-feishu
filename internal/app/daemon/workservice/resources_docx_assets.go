package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

// SavedDocumentAsset bytes are inside the version's immutable scope-bound
// Content BLOB and content digest, independent of live Drive tokens/caches.
type SavedDocumentAsset struct {
	Token     string `json:"token"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	SHA256    string `json:"sha256"`
	Content   []byte `json:"content"`
}

func (c *FeishuResourceConnector) captureDocumentAssets(ctx context.Context, blocks []*larkdocx.Block) ([]SavedDocumentAsset, []string) {
	assets := []SavedDocumentAsset{}
	issues := []string{}
	seen := map[string]bool{}
	total := 0
	for _, block := range blocks {
		if block == nil {
			continue
		}
		token, name, kind := "", "", ""
		if block.Image != nil && block.Image.Token != nil {
			token = *block.Image.Token
			name = "图片"
			kind = "image"
		}
		if block.File != nil && block.File.Token != nil {
			token = *block.File.Token
			name = scopeString(block.File.Name)
			kind = "file"
		}
		if block.Sheet != nil || block.Bitable != nil || block.Iframe != nil || block.Isv != nil || block.ReferenceSynced != nil || block.SourceSynced != nil || block.Board != nil || block.ReferenceBase != nil {
			issues = append(issues, "文档含外部嵌入内容，保存了原始引用但没有跨来源同版副本")
		}
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		if len(assets) >= 16 || total >= 32<<20 {
			issues = append(issues, "文档附件总数或总字节达到保存上限")
			continue
		}
		limit := int64(10 << 20)
		if remaining := int64(32<<20 - total); remaining < limit {
			limit = remaining
		}
		media, err := c.documents.DownloadDocumentMedia(ctx, feishu.ProjectDownloadDocumentMediaRequest{FileToken: token, MaxBytes: limit})
		if err != nil {
			issues = append(issues, fmt.Sprintf("文档%s未能独立保存：%v", kind, err))
			continue
		}
		if kind == "image" && !strings.HasPrefix(media.MediaType, "image/") {
			issues = append(issues, "图片素材没有返回可识别的图片字节")
			continue
		}
		if name == "" || name == "图片" {
			name = media.FileName
		}
		if name == "" {
			name = "文档附件"
		}
		hash := sha256.Sum256(media.Content)
		assets = append(assets, SavedDocumentAsset{Token: token, FileName: name, MediaType: media.MediaType, SHA256: hex.EncodeToString(hash[:]), Content: media.Content})
		total += len(media.Content)
	}
	return assets, issues
}
func scopeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
