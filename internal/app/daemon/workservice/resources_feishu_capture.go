package workservice

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

func (c *FeishuResourceConnector) captureDocument(ctx context.Context, resource work.Resource, token string, req CaptureResourceRequest, started time.Time) (CapturedResource, error) {
	if req.SheetID != "" || req.Range != "" || req.TableID != "" || len(req.FieldNames) > 0 || req.ValueRenderOption != "" {
		return CapturedResource{}, fmt.Errorf("Docx capture accepts only its explicit revision, not table/range options")
	}
	metadata, err := c.documents.GetDocument(ctx, feishu.ProjectDocumentID(token))
	if err != nil {
		return CapturedResource{}, err
	}
	revision := *metadata.RevisionID
	if req.RevisionID != nil && *req.RevisionID != -1 {
		revision = *req.RevisionID
	}
	if revision < 1 {
		return CapturedResource{}, fmt.Errorf("document revision must be -1 or >=1")
	}
	blocks := []*larkdocx.Block{}
	pageToken := ""
	requestIDs := []string{}
	seen := map[string]bool{}
	for {
		page, err := c.documents.ListDocumentBlocksPage(ctx, feishu.ProjectDocumentBlocksRequest{DocumentID: metadata.DocumentID, RevisionID: revision, PageSize: 500, PageToken: pageToken})
		if err != nil {
			return CapturedResource{}, err
		}
		blocks = append(blocks, page.Blocks...)
		requestIDs = append(requestIDs, page.RequestID)
		if !page.HasMore {
			break
		}
		if seen[page.NextPageToken] {
			return CapturedResource{}, fmt.Errorf("document pagination cycle")
		}
		seen[page.NextPageToken] = true
		pageToken = page.NextPageToken
	}
	assets, issues := c.captureDocumentAssets(ctx, blocks)
	content, err := json.Marshal(struct {
		Assets          []SavedDocumentAsset           `json:"assets,omitempty"`
		Issues          []string                       `json:"issues,omitempty"`
		CurrentMetadata feishu.ProjectDocumentMetadata `json:"current_metadata"`
		RevisionID      int                            `json:"revision_id"`
		Blocks          []*larkdocx.Block              `json:"blocks"`
		RequestIDs      []string                       `json:"request_ids"`
	}{assets, issues, metadata, revision, blocks, requestIDs})
	if err != nil {
		return CapturedResource{}, err
	}
	kind, consistency, complete := work.VersionImmutableSnapshot, work.ConsistencyAtomic, true
	// Media bytes are separate Drive observations; preserve the saved bytes
	// without claiming a cross-API atomic source revision.
	if len(assets) > 0 {
		kind = work.VersionObservedDataset
		consistency = work.ConsistencyObserved
	}
	if len(issues) > 0 {
		kind = work.VersionObservedDataset
		consistency = work.ConsistencyObserved
		complete = false
	}
	return CapturedResource{Kind: kind, Source: work.SourceRef{Kind: "feishu_docx", ID: resource.SourceIdentity, Version: strconv.Itoa(revision), EvidenceRef: metadata.RequestID}, Content: content, MediaType: "application/json", ObservationRange: "document revision " + strconv.Itoa(revision), Consistency: consistency, Complete: complete, StartedAt: started, CapturedAt: time.Now().UTC()}, nil
}
func (c *FeishuResourceConnector) captureSheet(ctx context.Context, resource work.Resource, token string, req CaptureResourceRequest, started time.Time) (CapturedResource, error) {
	if req.RevisionID != nil || req.TableID != "" || len(req.FieldNames) > 0 {
		return CapturedResource{}, fmt.Errorf("Sheets capture uses sheet/range options, not Docx revision or Bitable fields")
	}
	mode := req.ValueRenderOption
	if mode == "" {
		mode = "UnformattedValue"
	}
	values, err := c.sheets.ReadSheetRange(ctx, feishu.ProjectSheetRangeRequest{SpreadsheetToken: token, SheetID: req.SheetID, Range: req.Range, ValueRenderOption: mode})
	if err != nil {
		return CapturedResource{}, err
	}
	formulas, err := c.sheets.ReadSheetRange(ctx, feishu.ProjectSheetRangeRequest{SpreadsheetToken: token, SheetID: req.SheetID, Range: req.Range, ValueRenderOption: "Formula"})
	if err != nil {
		return CapturedResource{}, err
	}
	content, err := json.Marshal(struct {
		Values      feishu.ProjectSheetValues `json:"values"`
		Formulas    feishu.ProjectSheetValues `json:"formulas"`
		Consistency string                    `json:"consistency"`
	}{values, formulas, "separate observations; no cross-call atomicity guarantee"})
	if err != nil {
		return CapturedResource{}, err
	}
	sum := sha256.Sum256(content)
	return CapturedResource{Kind: work.VersionObservedDataset, Source: work.SourceRef{Kind: "feishu_sheets_observation", ID: resource.SourceIdentity, Version: "sha256:" + hex.EncodeToString(sum[:]), EvidenceRef: values.RequestID}, Content: content, MediaType: "application/json", ObservationRange: values.RequestedRange, Consistency: work.ConsistencyObserved, Complete: true, StartedAt: started, CapturedAt: time.Now().UTC()}, nil
}
func (c *FeishuResourceConnector) captureBitable(ctx context.Context, resource work.Resource, token string, req CaptureResourceRequest, started time.Time) (CapturedResource, error) {
	if req.TableID == "" {
		return CapturedResource{}, fmt.Errorf("Bitable capture requires an explicit table_id")
	}
	if req.RevisionID != nil || req.Range != "" || req.SheetID != "" || req.ValueRenderOption != "" {
		return CapturedResource{}, fmt.Errorf("Bitable capture uses table_id/field_names, not Docx revision or Sheets range")
	}
	fields, err := c.bitable.ListFields(ctx, token, req.TableID)
	if err != nil {
		return CapturedResource{}, err
	}
	records, err := c.bitable.ListRecords(ctx, token, req.TableID, req.FieldNames)
	if err != nil {
		return CapturedResource{}, err
	}
	content, err := json.Marshal(map[string]any{"table_id": req.TableID, "selected_fields": req.FieldNames, "schema": fields, "records": records, "consistency": "observed; existing adapter does not expose page-completeness receipts"})
	if err != nil {
		return CapturedResource{}, err
	}
	sum := sha256.Sum256(content)
	// Existing generic BitableAPI returns records without page receipts. Preserve
	// useful real data, but do not invent complete pagination or a native revision.
	return CapturedResource{Kind: work.VersionObservedDataset, Source: work.SourceRef{Kind: "feishu_bitable_observation", ID: resource.SourceIdentity + "/" + req.TableID, Version: "sha256:" + hex.EncodeToString(sum[:]), EvidenceRef: "table-observation"}, Content: content, MediaType: "application/json", ObservationRange: req.TableID, Consistency: work.ConsistencyObserved, Complete: false, StartedAt: started, CapturedAt: time.Now().UTC()}, nil
}
