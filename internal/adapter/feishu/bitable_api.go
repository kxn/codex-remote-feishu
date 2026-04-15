package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

const bitablePermissionDocType = "bitable"
const bitableBatchRecordLimit = 500

type BitableRecordUpdate struct {
	RecordID string
	Fields   map[string]any
}

type BitablePermissionMember struct {
	Perm     string
	PermType string
}

type BitableAPI interface {
	GetApp(context.Context, string) (*larkbitable.App, error)
	CreateApp(context.Context, string, string) (*larkbitable.App, error)
	ListTables(context.Context, string) ([]*larkbitable.AppTable, error)
	CreateTable(context.Context, string, *larkbitable.ReqTable) (*larkbitable.AppTable, error)
	RenameTable(context.Context, string, string, string) error
	ListFields(context.Context, string, string) ([]*larkbitable.AppTableField, error)
	CreateField(context.Context, string, string, *larkbitable.AppTableField) (*larkbitable.AppTableField, error)
	UpdateField(context.Context, string, string, string, *larkbitable.AppTableField) (*larkbitable.AppTableField, error)
	ListRecords(context.Context, string, string, []string) ([]*larkbitable.AppTableRecord, error)
	CreateRecord(context.Context, string, string, map[string]any) (*larkbitable.AppTableRecord, error)
	UpdateRecord(context.Context, string, string, string, map[string]any) (*larkbitable.AppTableRecord, error)
	BatchCreateRecords(context.Context, string, string, []map[string]any) ([]*larkbitable.AppTableRecord, error)
	BatchUpdateRecords(context.Context, string, string, []BitableRecordUpdate) ([]*larkbitable.AppTableRecord, error)
	ListPermissionMembers(context.Context, string, string) (map[string]BitablePermissionMember, error)
	GrantPermission(context.Context, string, string, string, string, string, string) error
	UpdatePermission(context.Context, string, string, string, string, string, string, string) error
}

type liveBitableAPI struct {
	client *lark.Client
}

func NewLiveBitableAPI(appID, appSecret string) BitableAPI {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return nil
	}
	return &liveBitableAPI{client: NewLarkClient(appID, appSecret)}
}

func (a *liveBitableAPI) GetApp(ctx context.Context, appToken string) (*larkbitable.App, error) {
	resp, err := a.client.Bitable.V1.App.Get(ctx, larkbitable.NewGetAppReqBuilder().
		AppToken(appToken).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app.get", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable app response data")
	}
	if resp.Data.App == nil {
		return nil, fmt.Errorf("missing bitable app metadata")
	}
	return &larkbitable.App{
		AppToken: resp.Data.App.AppToken,
		Name:     resp.Data.App.Name,
		Revision: resp.Data.App.Revision,
		TimeZone: resp.Data.App.TimeZone,
	}, nil
}

func (a *liveBitableAPI) CreateApp(ctx context.Context, name, timeZone string) (*larkbitable.App, error) {
	reqApp := larkbitable.NewReqAppBuilder().
		Name(strings.TrimSpace(name))
	if strings.TrimSpace(timeZone) != "" {
		reqApp.TimeZone(strings.TrimSpace(timeZone))
	}
	resp, err := a.client.Bitable.V1.App.Create(ctx, larkbitable.NewCreateAppReqBuilder().
		ReqApp(reqApp.Build()).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app.create", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable create app response data")
	}
	return resp.Data.App, nil
}

func (a *liveBitableAPI) ListTables(ctx context.Context, appToken string) ([]*larkbitable.AppTable, error) {
	var values []*larkbitable.AppTable
	pageToken := ""
	for {
		builder := larkbitable.NewListAppTableReqBuilder().
			AppToken(appToken).
			PageSize(200)
		if strings.TrimSpace(pageToken) != "" {
			builder.PageToken(pageToken)
		}
		resp, err := a.client.Bitable.V1.AppTable.List(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, newAPIError("bitable.v1.app_table.list", resp.ApiResp, resp.CodeError)
		}
		if resp.Data != nil {
			values = append(values, resp.Data.Items...)
			if resp.Data.HasMore != nil && *resp.Data.HasMore && strings.TrimSpace(stringValue(resp.Data.PageToken)) != "" {
				pageToken = stringValue(resp.Data.PageToken)
				continue
			}
		}
		return values, nil
	}
}

func (a *liveBitableAPI) CreateTable(ctx context.Context, appToken string, table *larkbitable.ReqTable) (*larkbitable.AppTable, error) {
	resp, err := a.client.Bitable.V1.AppTable.Create(ctx, larkbitable.NewCreateAppTableReqBuilder().
		AppToken(appToken).
		Body(&larkbitable.CreateAppTableReqBody{Table: table}).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app_table.create", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil || strings.TrimSpace(stringValue(resp.Data.TableId)) == "" {
		return nil, fmt.Errorf("missing bitable table id from create response")
	}
	return &larkbitable.AppTable{
		TableId: resp.Data.TableId,
		Name:    table.Name,
	}, nil
}

func (a *liveBitableAPI) RenameTable(ctx context.Context, appToken, tableID, name string) error {
	body, err := larkbitable.NewPatchAppTablePathReqBodyBuilder().
		Name(strings.TrimSpace(name)).
		Build()
	if err != nil {
		return err
	}
	resp, err := a.client.Bitable.V1.AppTable.Patch(ctx, larkbitable.NewPatchAppTableReqBuilder().
		AppToken(appToken).
		TableId(tableID).
		Body(body).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return newAPIError("bitable.v1.app_table.patch", resp.ApiResp, resp.CodeError)
	}
	return nil
}

func (a *liveBitableAPI) ListFields(ctx context.Context, appToken, tableID string) ([]*larkbitable.AppTableField, error) {
	var values []*larkbitable.AppTableField
	pageToken := ""
	for {
		builder := larkbitable.NewListAppTableFieldReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			PageSize(200)
		if strings.TrimSpace(pageToken) != "" {
			builder.PageToken(pageToken)
		}
		resp, err := a.client.Bitable.V1.AppTableField.List(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, newAPIError("bitable.v1.app_table_field.list", resp.ApiResp, resp.CodeError)
		}
		if resp.Data != nil {
			for _, item := range resp.Data.Items {
				if item == nil {
					continue
				}
				values = append(values, &larkbitable.AppTableField{
					FieldName: item.FieldName,
					Type:      item.Type,
					Property:  item.Property,
					IsPrimary: item.IsPrimary,
					FieldId:   item.FieldId,
					UiType:    item.UiType,
					IsHidden:  item.IsHidden,
				})
			}
			if resp.Data.HasMore != nil && *resp.Data.HasMore && strings.TrimSpace(stringValue(resp.Data.PageToken)) != "" {
				pageToken = stringValue(resp.Data.PageToken)
				continue
			}
		}
		return values, nil
	}
}

func (a *liveBitableAPI) CreateField(ctx context.Context, appToken, tableID string, field *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	resp, err := a.client.Bitable.V1.AppTableField.Create(ctx, larkbitable.NewCreateAppTableFieldReqBuilder().
		AppToken(appToken).
		TableId(tableID).
		AppTableField(field).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app_table_field.create", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable field response data")
	}
	return resp.Data.Field, nil
}

func (a *liveBitableAPI) UpdateField(ctx context.Context, appToken, tableID, fieldID string, field *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	resp, err := a.client.Bitable.V1.AppTableField.Update(ctx, larkbitable.NewUpdateAppTableFieldReqBuilder().
		AppToken(appToken).
		TableId(tableID).
		FieldId(fieldID).
		AppTableField(field).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app_table_field.update", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable update field response data")
	}
	return resp.Data.Field, nil
}

func (a *liveBitableAPI) ListRecords(ctx context.Context, appToken, tableID string, fieldNames []string) ([]*larkbitable.AppTableRecord, error) {
	var values []*larkbitable.AppTableRecord
	pageToken := ""
	fieldNamesQuery := ""
	if len(fieldNames) > 0 {
		raw, err := json.Marshal(fieldNames)
		if err != nil {
			return nil, err
		}
		fieldNamesQuery = string(raw)
	}
	for {
		builder := larkbitable.NewListAppTableRecordReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			PageSize(500)
		if fieldNamesQuery != "" {
			builder.FieldNames(fieldNamesQuery)
		}
		if strings.TrimSpace(pageToken) != "" {
			builder.PageToken(pageToken)
		}
		resp, err := a.client.Bitable.V1.AppTableRecord.List(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, newAPIError("bitable.v1.app_table_record.list", resp.ApiResp, resp.CodeError)
		}
		if resp.Data != nil {
			values = append(values, resp.Data.Items...)
			if resp.Data.HasMore != nil && *resp.Data.HasMore && strings.TrimSpace(stringValue(resp.Data.PageToken)) != "" {
				pageToken = stringValue(resp.Data.PageToken)
				continue
			}
		}
		return values, nil
	}
}

func (a *liveBitableAPI) CreateRecord(ctx context.Context, appToken, tableID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	resp, err := a.client.Bitable.V1.AppTableRecord.Create(ctx, larkbitable.NewCreateAppTableRecordReqBuilder().
		AppToken(appToken).
		TableId(tableID).
		AppTableRecord(larkbitable.NewAppTableRecordBuilder().Fields(fields).Build()).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app_table_record.create", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable create record response data")
	}
	return resp.Data.Record, nil
}

func (a *liveBitableAPI) UpdateRecord(ctx context.Context, appToken, tableID, recordID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	resp, err := a.client.Bitable.V1.AppTableRecord.Update(ctx, larkbitable.NewUpdateAppTableRecordReqBuilder().
		AppToken(appToken).
		TableId(tableID).
		RecordId(recordID).
		AppTableRecord(larkbitable.NewAppTableRecordBuilder().Fields(fields).Build()).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("bitable.v1.app_table_record.update", resp.ApiResp, resp.CodeError)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("missing bitable update record response data")
	}
	return resp.Data.Record, nil
}

func (a *liveBitableAPI) BatchCreateRecords(ctx context.Context, appToken, tableID string, values []map[string]any) ([]*larkbitable.AppTableRecord, error) {
	if len(values) == 0 {
		return nil, nil
	}
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for start := 0; start < len(values); start += bitableBatchRecordLimit {
		end := start + bitableBatchRecordLimit
		if end > len(values) {
			end = len(values)
		}
		items := make([]*larkbitable.AppTableRecord, 0, end-start)
		for _, fields := range values[start:end] {
			items = append(items, larkbitable.NewAppTableRecordBuilder().Fields(fields).Build())
		}
		resp, err := a.client.Bitable.V1.AppTableRecord.BatchCreate(ctx, larkbitable.NewBatchCreateAppTableRecordReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			Body(larkbitable.NewBatchCreateAppTableRecordReqBodyBuilder().
				Records(items).
				Build()).
			Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, newAPIError("bitable.v1.app_table_record.batch_create", resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("missing bitable batch create response data")
		}
		records = append(records, resp.Data.Records...)
	}
	return records, nil
}

func (a *liveBitableAPI) BatchUpdateRecords(ctx context.Context, appToken, tableID string, values []BitableRecordUpdate) ([]*larkbitable.AppTableRecord, error) {
	if len(values) == 0 {
		return nil, nil
	}
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for start := 0; start < len(values); start += bitableBatchRecordLimit {
		end := start + bitableBatchRecordLimit
		if end > len(values) {
			end = len(values)
		}
		items := make([]*larkbitable.AppTableRecord, 0, end-start)
		for _, update := range values[start:end] {
			recordID := strings.TrimSpace(update.RecordID)
			if recordID == "" {
				return nil, fmt.Errorf("missing record id in batch update")
			}
			items = append(items, larkbitable.NewAppTableRecordBuilder().
				RecordId(recordID).
				Fields(update.Fields).
				Build())
		}
		resp, err := a.client.Bitable.V1.AppTableRecord.BatchUpdate(ctx, larkbitable.NewBatchUpdateAppTableRecordReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			Body(larkbitable.NewBatchUpdateAppTableRecordReqBodyBuilder().
				Records(items).
				Build()).
			Build())
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, newAPIError("bitable.v1.app_table_record.batch_update", resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("missing bitable batch update response data")
		}
		records = append(records, resp.Data.Records...)
	}
	return records, nil
}

func (a *liveBitableAPI) ListPermissionMembers(ctx context.Context, token, docType string) (map[string]BitablePermissionMember, error) {
	resp, err := a.client.Drive.V1.PermissionMember.List(ctx, larkdrive.NewListPermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		Build())
	if err != nil {
		return nil, err
	}
	if !resp.Success() {
		return nil, newAPIError("drive.v1.permission_member.list", resp.ApiResp, resp.CodeError)
	}
	values := map[string]BitablePermissionMember{}
	if resp.Data == nil {
		return values, nil
	}
	for _, item := range resp.Data.Items {
		if item == nil {
			continue
		}
		memberType := strings.TrimSpace(stringValue(item.MemberType))
		memberID := strings.TrimSpace(stringValue(item.MemberId))
		if memberType == "" || memberID == "" {
			continue
		}
		values[memberType+":"+memberID] = BitablePermissionMember{
			Perm:     strings.TrimSpace(stringValue(item.Perm)),
			PermType: strings.TrimSpace(stringValue(item.PermType)),
		}
	}
	return values, nil
}

func (a *liveBitableAPI) GrantPermission(ctx context.Context, token, docType, memberType, memberID, principalType, perm string) error {
	perm = strings.TrimSpace(perm)
	if perm == "" {
		perm = "view"
	}
	resp, err := a.client.Drive.V1.PermissionMember.Create(ctx, larkdrive.NewCreatePermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		BaseMember(larkdrive.NewBaseMemberBuilder().
			MemberType(strings.TrimSpace(memberType)).
			MemberId(strings.TrimSpace(memberID)).
			Perm(perm).
			Type(strings.TrimSpace(principalType)).
			Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return newAPIError("drive.v1.permission_member.create", resp.ApiResp, resp.CodeError)
	}
	return nil
}

func (a *liveBitableAPI) UpdatePermission(ctx context.Context, token, docType, memberType, memberID, principalType, perm, permType string) error {
	perm = strings.TrimSpace(perm)
	if perm == "" {
		perm = "view"
	}
	body := larkdrive.NewBaseMemberBuilder().
		MemberType(strings.TrimSpace(memberType)).
		MemberId(strings.TrimSpace(memberID)).
		Perm(perm).
		Type(strings.TrimSpace(principalType))
	if strings.TrimSpace(permType) != "" {
		body.PermType(strings.TrimSpace(permType))
	}
	resp, err := a.client.Drive.V1.PermissionMember.Update(ctx, larkdrive.NewUpdatePermissionMemberReqBuilder().
		Token(token).
		Type(docType).
		MemberId(strings.TrimSpace(memberID)).
		BaseMember(body.Build()).
		Build())
	if err != nil {
		return err
	}
	if !resp.Success() {
		return newAPIError("drive.v1.permission_member.update", resp.ApiResp, resp.CodeError)
	}
	return nil
}
