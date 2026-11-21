package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

var projectCellRangePattern = regexp.MustCompile(`^([A-Z]{1,5})([1-9][0-9]*)(?::([A-Z]{1,5})([1-9][0-9]*))?$`)

func projectSheetRange(token, sheetID, cellRange string) (qualified string, rows, columns int, err error) {
	if err = projectBareToken(token, "spreadsheet token"); err != nil {
		return
	}
	if err = projectBareToken(sheetID, "sheet id"); err != nil {
		return
	}
	if strings.Contains(cellRange, "!") {
		parts := strings.Split(cellRange, "!")
		if len(parts) != 2 || parts[0] != sheetID {
			err = fmt.Errorf("range sheet id must match explicit sheet id")
			return
		}
		cellRange = parts[1]
	}
	match := projectCellRangePattern.FindStringSubmatch(cellRange)
	if match == nil {
		err = fmt.Errorf("explicit bounded A1 cell range required")
		return
	}
	startRow, parseErr := strconv.Atoi(match[2])
	if parseErr != nil {
		err = fmt.Errorf("invalid start row")
		return
	}
	endRow := startRow
	if match[4] != "" {
		endRow, parseErr = strconv.Atoi(match[4])
		if parseErr != nil {
			err = fmt.Errorf("invalid end row")
			return
		}
	}
	colNumber := func(value string) int {
		result := 0
		for _, ch := range value {
			result = result*26 + int(ch-'A'+1)
		}
		return result
	}
	startCol := colNumber(match[1])
	endCol := startCol
	if match[3] != "" {
		endCol = colNumber(match[3])
	}
	if endRow < startRow || endCol < startCol {
		err = fmt.Errorf("reversed cell range")
		return
	}
	return sheetID + "!" + cellRange, endRow - startRow + 1, endCol - startCol + 1, nil
}

type projectSheetsResponse struct {
	larkcore.CodeError
	Data      json.RawMessage `json:"data"`
	requestID string
}

// The Go SDK has no values data-plane service. Its authenticated Client.Do still
// runs inside DoSDK, retaining the same tenant credentials, rate and error broker.
// This private helper only serves the two concrete range operations below.
func (a *ProjectSheetsAPI) sheetValuesCall(ctx context.Context, api, token, method, rangePath string, query larkcore.QueryParams, body any) (*projectSheetsResponse, error) {
	return DoSDK(ctx, a.broker, projectResourceSpec(api, token, method == http.MethodGet), func(ctx context.Context, c *lark.Client) (*projectSheetsResponse, error) {
		path := "/open-apis/sheets/v2/spreadsheets/:spreadsheet_token/values"
		params := larkcore.PathParams{"spreadsheet_token": token}
		if rangePath != "" {
			path += "/:range"
			params["range"] = rangePath
		}
		resp, err := c.Do(ctx, &larkcore.ApiReq{HttpMethod: method, ApiPath: path, PathParams: params, QueryParams: query, Body: body, SupportedAccessTokenTypes: []larkcore.AccessTokenType{larkcore.AccessTokenTypeTenant}})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		envelope := &projectSheetsResponse{requestID: resp.RequestId()}
		if err := json.Unmarshal(resp.RawBody, envelope); err != nil {
			return envelope, fmt.Errorf("%s invalid JSON response: %w", api, err)
		}
		var status struct {
			Code *int `json:"code"`
		}
		if err := json.Unmarshal(resp.RawBody, &status); err != nil || status.Code == nil {
			return envelope, fmt.Errorf("%s returned missing status code", api)
		}
		if envelope.Code != 0 || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return envelope, newAPIError(api, resp, envelope.CodeError)
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return envelope, fmt.Errorf("%s returned nil data", api)
		}
		return envelope, nil
	})
}

func projectSheetValues(resp *projectSheetsResponse, result ProjectSheetValues) (ProjectSheetValues, error) {
	if resp == nil {
		return result, nil
	}
	result.RequestID = resp.requestID
	result.RawData = resp.Data
	var data struct {
		SpreadsheetToken string          `json:"spreadsheetToken"`
		Revision         json.RawMessage `json:"revision"`
		ValueRange       *struct {
			Range    string          `json:"range"`
			Revision json.RawMessage `json:"revision"`
			Values   [][]any         `json:"values"`
		} `json:"valueRange"`
		UpdatedRange   string `json:"updatedRange"`
		UpdatedRows    *int   `json:"updatedRows"`
		UpdatedColumns *int   `json:"updatedColumns"`
		UpdatedCells   *int   `json:"updatedCells"`
	}
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return result, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(resp.Data))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return result, fmt.Errorf("invalid sheet values data: %w", err)
	}
	if data.SpreadsheetToken != "" {
		result.SpreadsheetToken = data.SpreadsheetToken
	}
	result.Revision = data.Revision
	result.UpdatedRows = data.UpdatedRows
	result.UpdatedColumns = data.UpdatedColumns
	result.UpdatedCells = data.UpdatedCells
	result.Range = data.UpdatedRange
	if data.ValueRange != nil {
		result.Range = data.ValueRange.Range
		result.RangeRevision = data.ValueRange.Revision
		result.Values = data.ValueRange.Values
	}
	return result, nil
}

// ReadSheetRange retains the requested render mode. Formula and calculated reads
// are separate observations; there is no same-revision guarantee between them.
func (a *ProjectSheetsAPI) ReadSheetRange(ctx context.Context, req ProjectSheetRangeRequest) (ProjectSheetValues, error) {
	result := ProjectSheetValues{SpreadsheetToken: req.SpreadsheetToken, SheetID: req.SheetID, ValueRenderOption: req.ValueRenderOption, DateTimeRenderOption: req.DateTimeRenderOption}
	qualified, _, _, err := projectSheetRange(req.SpreadsheetToken, req.SheetID, req.Range)
	if err != nil {
		return result, err
	}
	result.RequestedRange = qualified
	query := larkcore.QueryParams{"user_id_type": []string{"open_id"}}
	switch req.ValueRenderOption {
	case "", "ToString", "FormattedValue", "Formula", "UnformattedValue":
	default:
		return result, fmt.Errorf("unsupported value render option")
	}
	if req.ValueRenderOption != "" {
		query.Set("valueRenderOption", req.ValueRenderOption)
	}
	if req.DateTimeRenderOption != "" {
		if req.DateTimeRenderOption != "FormattedString" {
			return result, fmt.Errorf("unsupported date time render option")
		}
		query.Set("dateTimeRenderOption", req.DateTimeRenderOption)
	}
	resp, callErr := a.sheetValuesCall(ctx, "sheets.v2.spreadsheet.values.get", req.SpreadsheetToken, http.MethodGet, qualified, query, nil)
	result, parseErr := projectSheetValues(resp, result)
	if callErr != nil {
		return result, callErr
	}
	if parseErr != nil {
		return result, parseErr
	}
	if result.Range == "" {
		return result, fmt.Errorf("missing returned sheet range")
	}
	return result, nil
}

// WriteSheetRange writes exactly one explicit, bounded rectangle. Matrix shape
// must equal that rectangle; values are forwarded without rewriting formulas or
// adjacent human cells. No generic idempotency key or expected revision exists.
func (a *ProjectSheetsAPI) WriteSheetRange(ctx context.Context, req ProjectSheetWriteRequest) (ProjectSheetValues, error) {
	result := ProjectSheetValues{SpreadsheetToken: req.SpreadsheetToken, SheetID: req.SheetID}
	qualified, rows, cols, err := projectSheetRange(req.SpreadsheetToken, req.SheetID, req.Range)
	if err != nil {
		return result, err
	}
	result.RequestedRange = qualified
	if len(req.Values) != rows {
		return result, fmt.Errorf("values row count must match explicit range")
	}
	for _, row := range req.Values {
		if len(row) != cols {
			return result, fmt.Errorf("values column count must match explicit range")
		}
	}
	body := map[string]any{"valueRange": map[string]any{"range": qualified, "values": req.Values}}
	resp, callErr := a.sheetValuesCall(ctx, "sheets.v2.spreadsheet.values.update", req.SpreadsheetToken, http.MethodPut, "", nil, body)
	result, parseErr := projectSheetValues(resp, result)
	if callErr != nil {
		return result, callErr
	}
	if parseErr != nil {
		return result, parseErr
	}
	if result.Range == "" {
		return result, fmt.Errorf("missing updated sheet range")
	}
	return result, nil
}
