package workprovision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func renderIndex(index HomeIndex) (string, string) {
	states := map[work.ProjectState]string{work.ProjectProvisioning: "正在建立", work.ProjectActive: "进行中", work.ProjectDegraded: "部分工作待恢复", work.ProjectPausing: "正在暂停", work.ProjectPaused: "已暂停", work.ProjectArchiving: "正在归档", work.ProjectArchived: "已归档"}
	text := "项目：" + index.ProjectName + "\n项目状态：" + states[index.ProjectState] + "\n可用同事：" + strconv.Itoa(index.ReadyMembers) + " / " + strconv.Itoa(index.TotalMembers) + "\n待办：" + strconv.Itoa(index.PendingTasks) + "\n进行中：" + strconv.Itoa(index.ActiveTasks) + "\n已完成：" + strconv.Itoa(index.DoneTasks)
	for _, resource := range index.Resources {
		text += "\n\n资料：" + resource.Label
		if resource.URL != "" {
			text += "\n原件链接（访问以飞书为准）：" + resource.URL
		}
	}
	for _, delivery := range index.Deliveries {
		text += "\n\n成果：" + delivery.Summary
		if delivery.NeededAction != "" {
			text += "\n需要您：" + delivery.NeededAction
		}
		for _, resource := range delivery.Resources {
			text += "\n" + resource.Label
			if resource.Version != "" {
				text += " · 已保存版本 " + resource.Version
			}
			if resource.URL != "" {
				text += "\n原件链接（内容和访问可能变化）：" + resource.URL
			}
		}
	}
	if len(index.Deliveries) > 0 {
		text += "\n\n从群导航打开「查看工作 → 查看成果与历史」，可回看发布时保存的版本。"
	}
	status := "索引更新时间：" + index.UpdatedAt.UTC().Format(time.RFC3339) + "\n同步状态：" + index.SyncState
	return text, status
}
func textBlock(text string) *larkdocx.Block {
	kind := 2
	return &larkdocx.Block{BlockType: &kind, Text: &larkdocx.Text{Elements: []*larkdocx.TextElement{{TextRun: &larkdocx.TextRun{Content: &text}}}}}
}
func (p *FeishuPlatform) AppendHomeIndex(ctx context.Context, id string, index HomeIndex, clientToken string) (HomeBlockIDs, error) {
	text, status := renderIndex(index)
	receipt, err := p.documents.AppendDocumentBlocks(ctx, feishu.ProjectAppendDocumentBlocksRequest{DocumentID: feishu.ProjectDocumentID(id), ParentBlockID: id, RevisionID: -1, Index: -1, ClientToken: clientToken, Blocks: []*larkdocx.Block{textBlock(text), textBlock(status)}})
	result := HomeBlockIDs{RequestID: receipt.RequestID}
	if receipt.RevisionID != nil {
		result.Revision = *receipt.RevisionID
	}
	if len(receipt.Blocks) > 0 && receipt.Blocks[0] != nil && receipt.Blocks[0].BlockId != nil {
		result.IndexID = *receipt.Blocks[0].BlockId
	}
	if len(receipt.Blocks) > 1 && receipt.Blocks[1] != nil && receipt.Blocks[1].BlockId != nil {
		result.StatusID = *receipt.Blocks[1].BlockId
	}
	if err == nil && (result.IndexID == "" || result.StatusID == "") {
		err = fmt.Errorf("platform omitted managed block IDs")
	}
	return result, err
}
func homeBlock(block *larkdocx.Block) (HomeBlock, error) {
	if block == nil || block.BlockId == nil || block.Text == nil {
		return HomeBlock{}, fmt.Errorf("managed home block is missing or no longer plain text")
	}
	raw, err := json.Marshal(block)
	if err != nil {
		return HomeBlock{}, err
	}
	sum := sha256.Sum256(raw)
	result := HomeBlock{ID: *block.BlockId, Fingerprint: hex.EncodeToString(sum[:])}
	for _, element := range block.Text.Elements {
		if element == nil || element.TextRun == nil || element.TextRun.Content == nil {
			return HomeBlock{}, fmt.Errorf("managed block contains human or unsupported content")
		}
		result.Text += *element.TextRun.Content
	}
	return result, nil
}
func (p *FeishuPlatform) ReadHomeBlocks(ctx context.Context, documentID, indexID, statusID string) (HomeBlocks, error) {
	result := HomeBlocks{}
	doc, err := p.documents.GetDocument(ctx, feishu.ProjectDocumentID(documentID))
	if err != nil {
		return result, err
	}
	result.Revision = *doc.RevisionID
	first, err := p.documents.GetProjectDocumentBlock(ctx, doc.DocumentID, indexID, result.Revision)
	if err != nil {
		return result, err
	}
	result.Index, err = homeBlock(first.Block)
	if err != nil {
		return result, err
	}
	second, err := p.documents.GetProjectDocumentBlock(ctx, doc.DocumentID, statusID, result.Revision)
	if err != nil {
		return result, err
	}
	result.Status, err = homeBlock(second.Block)
	result.RequestID = second.RequestID
	return result, err
}
func (p *FeishuPlatform) UpdateHomeIndex(ctx context.Context, documentID, indexID, statusID string, index HomeIndex, clientToken string) (HomeBlocks, error) {
	text, status := renderIndex(index)
	_, err := p.documents.UpdateDocumentBlocks(ctx, feishu.ProjectUpdateDocumentBlocksRequest{DocumentID: feishu.ProjectDocumentID(documentID), RevisionID: -1, ClientToken: clientToken, Requests: []*larkdocx.UpdateBlockRequest{
		{BlockId: &indexID, UpdateTextElements: &larkdocx.UpdateTextElementsRequest{Elements: textBlock(text).Text.Elements}},
		{BlockId: &statusID, UpdateTextElements: &larkdocx.UpdateTextElementsRequest{Elements: textBlock(status).Text.Elements}},
	}})
	if err != nil {
		return HomeBlocks{}, err
	}
	return p.ReadHomeBlocks(ctx, documentID, indexID, statusID)
}
