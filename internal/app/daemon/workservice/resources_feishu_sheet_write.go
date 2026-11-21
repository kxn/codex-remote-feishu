package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
)

func resourceSpreadsheetToken(resource work.Resource) (string, error) {
	if !strings.HasPrefix(resource.SourceIdentity, "sheets:") {
		return "", fmt.Errorf("registered resource is not a spreadsheet")
	}
	token := strings.TrimPrefix(resource.SourceIdentity, "sheets:")
	if token == "" {
		return "", fmt.Errorf("empty spreadsheet identity")
	}
	return token, nil
}
func sheetReadHash(values feishu.ProjectSheetValues) (string, error) {
	return resourceJSONHash(struct {
		Range                   string
		Values                  [][]any
		Revision, RangeRevision json.RawMessage
	}{values.Range, values.Values, values.Revision, values.RangeRevision})
}
func (c *FeishuResourceConnector) ReadResourceSheet(ctx context.Context, resource work.Resource, req ReadResourceSheetRequest) (ResourceSheetInspection, error) {
	token, err := resourceSpreadsheetToken(resource)
	if err != nil {
		return ResourceSheetInspection{}, err
	}
	values, err := c.sheets.ReadSheetRange(ctx, feishu.ProjectSheetRangeRequest{SpreadsheetToken: token, SheetID: req.SheetID, Range: req.Range, ValueRenderOption: "Formula"})
	result := ResourceSheetInspection{Values: values}
	if err != nil {
		return result, err
	}
	result.DataSHA256, err = sheetReadHash(values)
	return result, err
}
func (c *FeishuResourceConnector) WriteResourceSheet(ctx context.Context, resource work.Resource, req WriteResourceSheetRequest) (ResourceWriteReceipt, error) {
	current, err := c.ReadResourceSheet(ctx, resource, ReadResourceSheetRequest{ResourceID: resource.ID, SheetID: req.SheetID, Range: req.Range})
	if err != nil {
		return ResourceWriteReceipt{}, err
	}
	if current.DataSHA256 != req.ExpectedDataSHA256 {
		return ResourceWriteReceipt{}, fmt.Errorf("sheet range changed; read the exact current range before writing")
	}
	token, err := resourceSpreadsheetToken(resource)
	if err != nil {
		return ResourceWriteReceipt{}, err
	}
	// Validate the full cell rectangle before marking a write attempted. The API
	// exposes no expected-revision parameter; this preflight is not a CAS.
	if err = feishu.ValidateProjectSheetWrite(feishu.ProjectSheetWriteRequest{SpreadsheetToken: token, SheetID: req.SheetID, Range: req.Range, Values: req.Values}); err != nil {
		return ResourceWriteReceipt{}, err
	}
	values, err := c.sheets.WriteSheetRange(ctx, feishu.ProjectSheetWriteRequest{SpreadsheetToken: token, SheetID: req.SheetID, Range: req.Range, Values: req.Values})
	raw, _ := json.Marshal(values)
	return ResourceWriteReceipt{Attempted: true, ExternalID: token, RawEvidence: raw}, err
}
