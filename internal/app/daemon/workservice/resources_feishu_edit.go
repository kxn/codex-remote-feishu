package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

func documentResourceID(resource work.Resource) (feishu.ProjectDocumentID, error) {
	if !strings.HasPrefix(resource.SourceIdentity, "docx:") {
		return "", fmt.Errorf("registered resource is not a Docx object")
	}
	id := strings.TrimPrefix(resource.SourceIdentity, "docx:")
	if id == "" {
		return "", fmt.Errorf("empty Docx identity")
	}
	return feishu.ProjectDocumentID(id), nil
}
func resourceBlockText(b *larkdocx.Block) *larkdocx.Text {
	for _, text := range []*larkdocx.Text{b.Text, b.Heading1, b.Heading2, b.Heading3, b.Heading4, b.Heading5, b.Heading6, b.Heading7, b.Heading8, b.Heading9, b.Bullet, b.Ordered, b.Code, b.Quote, b.Todo} {
		if text != nil {
			return text
		}
	}
	return nil
}
func resourceJSONHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func resourceBlockProjection(block *larkdocx.Block, req InspectResourceDocumentRequest) (ResourceDocumentBlock, error) {
	result := ResourceDocumentBlock{}
	if block == nil || block.BlockId == nil {
		return result, fmt.Errorf("platform returned block without identity")
	}
	result.ID = *block.BlockId
	if block.BlockType != nil {
		result.Kind = *block.BlockType
	}
	result.ChildCount = len(block.Children)
	var err error
	result.SHA256, err = resourceJSONHash(block)
	if err != nil {
		return result, err
	}
	text := resourceBlockText(block)
	if text == nil {
		return result, nil
	}
	result.RunCount = len(text.Elements)
	if req.TextRunIndex != nil {
		index := *req.TextRunIndex
		if index < 0 || index >= len(text.Elements) || text.Elements[index] == nil || text.Elements[index].TextRun == nil {
			return result, fmt.Errorf("selected run is not an existing text run")
		}
		value := ""
		if text.Elements[index].TextRun.Content != nil {
			value = *text.Elements[index].TextRun.Content
		}
		runes := []rune(value)
		if req.Offset < 0 || req.Offset > len(runes) || req.Limit < 0 || req.Limit > 8000 {
			return result, fmt.Errorf("invalid text character page")
		}
		limit := req.Limit
		if limit == 0 {
			limit = 4000
		}
		end := req.Offset + limit
		if end > len(runes) {
			end = len(runes)
		}
		page := ContentPage{ID: result.ID, SHA256: result.SHA256, MediaType: "text/plain", Text: string(runes[req.Offset:end]), Offset: req.Offset, Total: len(runes)}
		if end < len(runes) {
			page.NextOffset = &end
		}
		result.Text = &page
		return result, nil
	}
	for index, element := range text.Elements {
		run := ResourceDocumentRun{Index: index, Kind: "non_text"}
		if element != nil && element.TextRun != nil {
			run.Kind = "text"
			value := ""
			if element.TextRun.Content != nil {
				value = *element.TextRun.Content
			}
			chars := []rune(value)
			run.Characters = len(chars)
			if len(chars) > 160 {
				chars = chars[:160]
			}
			run.Summary = string(chars)
		}
		result.Runs = append(result.Runs, run)
	}
	return result, nil
}
func (c *FeishuResourceConnector) InspectResourceDocument(ctx context.Context, resource work.Resource, req InspectResourceDocumentRequest) (ResourceDocumentInspection, error) {
	if req.PageToken != "" && req.DocumentRevision < 1 {
		return ResourceDocumentInspection{}, fmt.Errorf("continuation requires the exact previous document revision")
	}
	id, err := documentResourceID(resource)
	if err != nil {
		return ResourceDocumentInspection{}, err
	}
	meta, err := c.documents.GetDocument(ctx, id)
	if err != nil {
		return ResourceDocumentInspection{}, err
	}
	if meta.RevisionID == nil {
		return ResourceDocumentInspection{}, fmt.Errorf("Docx metadata returned no revision")
	}
	revision := req.DocumentRevision
	if revision == 0 {
		revision = *meta.RevisionID
	}
	if revision < 1 {
		return ResourceDocumentInspection{}, fmt.Errorf("explicit positive document revision required")
	}
	result := ResourceDocumentInspection{DocumentID: string(id), ObservedRevision: *meta.RevisionID, RequestedRevision: revision}
	var blocks []*larkdocx.Block
	if req.BlockID != "" {
		if req.PageToken != "" {
			return result, fmt.Errorf("block selection cannot carry a document page token")
		}
		block, e := c.documents.GetDocumentBlock(ctx, id, req.BlockID, revision)
		if e != nil {
			return result, e
		}
		blocks = []*larkdocx.Block{block}
	} else {
		if req.TextRunIndex != nil {
			return result, fmt.Errorf("text-run selection requires an explicit block")
		}
		size := req.PageSize
		if size == 0 {
			size = 20
		}
		if size < 1 || size > 50 {
			return result, fmt.Errorf("document page_size must be1..50")
		}
		page, e := c.documents.ListDocumentBlocksPage(ctx, feishu.ProjectDocumentBlocksRequest{DocumentID: id, RevisionID: revision, PageSize: size, PageToken: req.PageToken})
		if e != nil {
			return result, e
		}
		blocks = page.Blocks
		result.HasMore = page.HasMore
		result.NextPageToken = page.NextPageToken
	}
	for _, block := range blocks {
		projection, e := resourceBlockProjection(block, req)
		if e != nil {
			return result, e
		}
		result.Blocks = append(result.Blocks, projection)
	}
	return result, nil
}
func (c *FeishuResourceConnector) observedDocument(ctx context.Context, resource work.Resource, revision int) (feishu.ProjectDocumentID, error) {
	id, err := documentResourceID(resource)
	if err != nil {
		return "", err
	}
	meta, err := c.documents.GetDocument(ctx, id)
	if err != nil {
		return "", err
	}
	if meta.RevisionID == nil || *meta.RevisionID != revision {
		return "", fmt.Errorf("document revision changed; inspect the current document before editing")
	}
	return id, nil
}
func mutationResourceReceipt(m feishu.ProjectDocumentBlocksMutation) ResourceWriteReceipt {
	r := ResourceWriteReceipt{Attempted: true, ExternalID: string(m.DocumentID), DocumentRevision: m.RevisionID}
	for _, b := range m.Blocks {
		if b != nil && b.BlockId != nil {
			r.BlockIDs = append(r.BlockIDs, *b.BlockId)
		}
	}
	r.RawEvidence, _ = json.Marshal(m)
	return r
}
func (c *FeishuResourceConnector) AppendResourceDocument(ctx context.Context, resource work.Resource, req AppendResourceDocumentRequest, token string) (ResourceWriteReceipt, error) {
	id, err := c.observedDocument(ctx, resource, req.ObservedDocumentRevision)
	if err != nil {
		return ResourceWriteReceipt{}, err
	}
	// Read the actual selected parent, not a guessed latest block.
	if _, err = c.documents.GetDocumentBlock(ctx, id, req.ParentBlockID, req.ObservedDocumentRevision); err != nil {
		return ResourceWriteReceipt{}, err
	}
	blocks := make([]*larkdocx.Block, 0, len(req.Paragraphs))
	kind := 2
	for _, value := range req.Paragraphs {
		value := value
		blocks = append(blocks, &larkdocx.Block{BlockType: &kind, Text: &larkdocx.Text{Elements: []*larkdocx.TextElement{{TextRun: &larkdocx.TextRun{Content: &value}}}}})
	}
	index := -1
	if req.Index != nil {
		index = *req.Index
	}
	mutation, err := c.documents.AppendDocumentBlocks(ctx, feishu.ProjectAppendDocumentBlocksRequest{DocumentID: id, ParentBlockID: req.ParentBlockID, RevisionID: req.ObservedDocumentRevision, Index: index, ClientToken: token, Blocks: blocks})
	return mutationResourceReceipt(mutation), err
}
func (c *FeishuResourceConnector) UpdateResourceDocument(ctx context.Context, resource work.Resource, req UpdateResourceDocumentRequest, token string) (ResourceWriteReceipt, error) {
	id, err := c.observedDocument(ctx, resource, req.ObservedDocumentRevision)
	if err != nil {
		return ResourceWriteReceipt{}, err
	}
	blocks := map[string]*larkdocx.Block{}
	hashes := map[string]string{}
	order := []string{}
	for _, edit := range req.Edits {
		block := blocks[edit.BlockID]
		if block == nil {
			block, err = c.documents.GetDocumentBlock(ctx, id, edit.BlockID, req.ObservedDocumentRevision)
			if err != nil {
				return ResourceWriteReceipt{}, err
			}
			hash, e := resourceJSONHash(block)
			if e != nil {
				return ResourceWriteReceipt{}, e
			}
			hashes[edit.BlockID] = hash
			blocks[edit.BlockID] = block
			order = append(order, edit.BlockID)
		}
		if hashes[edit.BlockID] != edit.ExpectedBlockSHA256 {
			return ResourceWriteReceipt{}, fmt.Errorf("block changed; inspect its exact full-block hash before editing")
		}
		text := resourceBlockText(block)
		if text == nil || edit.TextRunIndex < 0 || edit.TextRunIndex >= len(text.Elements) || text.Elements[edit.TextRunIndex] == nil || text.Elements[edit.TextRunIndex].TextRun == nil {
			return ResourceWriteReceipt{}, fmt.Errorf("edit target is not an existing text run")
		}
		value := edit.Text
		text.Elements[edit.TextRunIndex].TextRun.Content = &value
	}
	requests := make([]*larkdocx.UpdateBlockRequest, 0, len(order))
	for _, blockID := range order {
		blockID := blockID
		requests = append(requests, &larkdocx.UpdateBlockRequest{BlockId: &blockID, UpdateTextElements: &larkdocx.UpdateTextElementsRequest{Elements: resourceBlockText(blocks[blockID]).Elements}})
	}
	// revision_id is platform evidence, not a cross-request compare-and-swap.
	mutation, err := c.documents.UpdateDocumentBlocks(ctx, feishu.ProjectUpdateDocumentBlocksRequest{DocumentID: id, RevisionID: req.ObservedDocumentRevision, ClientToken: token, Requests: requests})
	return mutationResourceReceipt(mutation), err
}
