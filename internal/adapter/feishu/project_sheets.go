package feishu

import (
	"context"
	"encoding/json"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larksheets "github.com/larksuite/oapi-sdk-go/v3/service/sheets/v3"
)

type ProjectSheetsAPI struct{ broker *FeishuCallBroker }

func NewProjectSheetsAPI(broker *FeishuCallBroker) *ProjectSheetsAPI {
	return &ProjectSheetsAPI{broker: broker}
}
func (g *LiveGateway) ProjectSheetsAPI() *ProjectSheetsAPI {
	if g == nil {
		return nil
	}
	return NewProjectSheetsAPI(g.broker)
}

type ProjectSpreadsheetMetadata struct {
	SpreadsheetToken, Title, OwnerID, URL, RequestID string
	// The v3 metadata endpoint exposes no revision. No synthetic revision is added.
}

type ProjectSheetRangeRequest struct {
	SpreadsheetToken     string
	SheetID              string
	Range                string // A1 or A1:B2, optionally prefixed by the matching SheetID!.
	ValueRenderOption    string // Formula retains formula representation; no conversion.
	DateTimeRenderOption string
}

type ProjectSheetWriteRequest struct {
	SpreadsheetToken, SheetID, Range string
	Values                           [][]any // Platform cell values, including rich/formula values, unchanged.
}

// ProjectSheetValues retains partial platform data even on error; callers must
// check err and reconcile uncertain writes before publishing success.
type ProjectSheetValues struct {
	SpreadsheetToken                          string
	SheetID                                   string
	RequestedRange                            string
	Range                                     string  // Actual valueRange.range or updatedRange reported by the platform.
	Values                                    [][]any // json.Number preserves numeric precision; rich objects stay intact.
	ValueRenderOption, DateTimeRenderOption   string
	Revision                                  json.RawMessage // Raw outer revision evidence; not a CAS token.
	RangeRevision                             json.RawMessage // Raw valueRange revision, separately retained.
	UpdatedRows, UpdatedColumns, UpdatedCells *int
	RequestID                                 string
	RawData                                   json.RawMessage // Includes platform fields not normalized above.
	// Writes report receipts, not read-back values or atomic historical snapshots.
}

func (a *ProjectSheetsAPI) GetSpreadsheet(ctx context.Context, token string) (ProjectSpreadsheetMetadata, error) {
	if err := projectBareToken(token, "spreadsheet token"); err != nil {
		return ProjectSpreadsheetMetadata{}, err
	}
	const api = "sheets.v3.spreadsheet.get"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, token, true), func(ctx context.Context, c *lark.Client) (*larksheets.GetSpreadsheetResp, error) {
		resp, err := c.Sheets.V3.Spreadsheet.Get(ctx, larksheets.NewGetSpreadsheetReqBuilder().SpreadsheetToken(token).UserIdType("open_id").Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || resp.Data.Spreadsheet == nil {
			return resp, fmt.Errorf("%s returned missing spreadsheet", api)
		}
		return resp, nil
	})
	if resp == nil {
		return ProjectSpreadsheetMetadata{}, err
	}
	result := ProjectSpreadsheetMetadata{RequestID: resp.RequestId()}
	if resp.Data == nil || resp.Data.Spreadsheet == nil {
		return result, err
	}
	sheet := resp.Data.Spreadsheet
	result.SpreadsheetToken = scopeStringValue(sheet.Token)
	result.Title = scopeStringValue(sheet.Title)
	result.OwnerID = scopeStringValue(sheet.OwnerId)
	result.URL = scopeStringValue(sheet.Url)
	if err != nil {
		return result, err
	}
	if result.SpreadsheetToken == "" {
		return result, fmt.Errorf("missing spreadsheet token")
	}
	return result, nil
}

// ListSpreadsheetSheets obtains stable sheet IDs for concrete range operations.
// This SDK endpoint returns all sheet metadata in one response, without paging.
func (a *ProjectSheetsAPI) ListSpreadsheetSheets(ctx context.Context, token string) ([]*larksheets.Sheet, error) {
	if err := projectBareToken(token, "spreadsheet token"); err != nil {
		return nil, err
	}
	const api = "sheets.v3.spreadsheet.sheet.query"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, token, true), func(ctx context.Context, c *lark.Client) (*larksheets.QuerySpreadsheetSheetResp, error) {
		resp, err := c.Sheets.V3.SpreadsheetSheet.Query(ctx, larksheets.NewQuerySpreadsheetSheetReqBuilder().SpreadsheetToken(token).Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if resp == nil || resp.Data == nil {
		return nil, err
	}
	return resp.Data.Sheets, err
}
