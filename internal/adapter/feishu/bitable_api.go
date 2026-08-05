package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
	broker *FeishuCallBroker
}

func NewLiveBitableAPI(gatewayID, appID, appSecret string) BitableAPI {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return nil
	}
	client := NewLarkClient(appID, appSecret)
	return &liveBitableAPI{
		client: client,
		broker: NewFeishuCallBroker(gatewayID, client),
	}
}

func bitableResourceKey(appToken, tableID string) FeishuResourceKey {
	return FeishuResourceKey{
		TableID: xutil.FirstNonEmpty(strings.TrimSpace(tableID), strings.TrimSpace(appToken)),
	}
}

func driveDocResourceKey(token string) FeishuResourceKey {
	return FeishuResourceKey{DocToken: strings.TrimSpace(token)}
}

func (a *liveBitableAPI) GetApp(ctx context.Context, appToken string) (*larkbitable.App, error) {
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app.get",
		Class:       CallClassBitable,
		Priority:    CallPriorityReadAssist,
		ResourceKey: bitableResourceKey(appToken, ""),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.GetAppResp, error) {
		resp, err := client.Bitable.V1.App.Get(callCtx, larkbitable.NewGetAppReqBuilder().
			AppToken(appToken).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:        "bitable.v1.app.create",
		Class:      CallClassBitable,
		Priority:   CallPriorityBackground,
		Retry:      RetryOff,
		Permission: PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.CreateAppResp, error) {
		resp, err := client.Bitable.V1.App.Create(callCtx, larkbitable.NewCreateAppReqBuilder().
			ReqApp(reqApp.Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:         "bitable.v1.app_table.list",
			Class:       CallClassBitable,
			Priority:    CallPriorityReadAssist,
			ResourceKey: bitableResourceKey(appToken, ""),
			Retry:       RetrySafe,
			Permission:  PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkbitable.ListAppTableResp, error) {
			resp, err := client.Bitable.V1.AppTable.List(callCtx, builder.Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table.create",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, ""),
		Retry:       RetryOff,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.CreateAppTableResp, error) {
		resp, err := client.Bitable.V1.AppTable.Create(callCtx, larkbitable.NewCreateAppTableReqBuilder().
			AppToken(appToken).
			Body(&larkbitable.CreateAppTableReqBody{Table: table}).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table.patch",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, tableID),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.PatchAppTableResp, error) {
		resp, err := client.Bitable.V1.AppTable.Patch(callCtx, larkbitable.NewPatchAppTableReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			Body(body).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:         "bitable.v1.app_table_field.list",
			Class:       CallClassBitable,
			Priority:    CallPriorityReadAssist,
			ResourceKey: bitableResourceKey(appToken, tableID),
			Retry:       RetrySafe,
			Permission:  PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkbitable.ListAppTableFieldResp, error) {
			resp, err := client.Bitable.V1.AppTableField.List(callCtx, builder.Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table_field.create",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, tableID),
		Retry:       RetryOff,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.CreateAppTableFieldResp, error) {
		resp, err := client.Bitable.V1.AppTableField.Create(callCtx, larkbitable.NewCreateAppTableFieldReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			AppTableField(field).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table_field.update",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, tableID),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.UpdateAppTableFieldResp, error) {
		resp, err := client.Bitable.V1.AppTableField.Update(callCtx, larkbitable.NewUpdateAppTableFieldReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			FieldId(fieldID).
			AppTableField(field).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:         "bitable.v1.app_table_record.list",
			Class:       CallClassBitable,
			Priority:    CallPriorityReadAssist,
			ResourceKey: bitableResourceKey(appToken, tableID),
			Retry:       RetrySafe,
			Permission:  PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkbitable.ListAppTableRecordResp, error) {
			resp, err := client.Bitable.V1.AppTableRecord.List(callCtx, builder.Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table_record.create",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, tableID),
		Retry:       RetryOff,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.CreateAppTableRecordResp, error) {
		resp, err := client.Bitable.V1.AppTableRecord.Create(callCtx, larkbitable.NewCreateAppTableRecordReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			AppTableRecord(larkbitable.NewAppTableRecordBuilder().Fields(fields).Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "bitable.v1.app_table_record.update",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: bitableResourceKey(appToken, tableID),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkbitable.UpdateAppTableRecordResp, error) {
		resp, err := client.Bitable.V1.AppTableRecord.Update(callCtx, larkbitable.NewUpdateAppTableRecordReqBuilder().
			AppToken(appToken).
			TableId(tableID).
			RecordId(recordID).
			AppTableRecord(larkbitable.NewAppTableRecordBuilder().Fields(fields).Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:         "bitable.v1.app_table_record.batch_create",
			Class:       CallClassBitable,
			Priority:    CallPriorityBackground,
			ResourceKey: bitableResourceKey(appToken, tableID),
			Retry:       RetryOff,
			Permission:  PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkbitable.BatchCreateAppTableRecordResp, error) {
			resp, err := client.Bitable.V1.AppTableRecord.BatchCreate(callCtx, larkbitable.NewBatchCreateAppTableRecordReqBuilder().
				AppToken(appToken).
				TableId(tableID).
				Body(larkbitable.NewBatchCreateAppTableRecordReqBodyBuilder().
					Records(items).
					Build()).
				Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
		resp, err := DoSDK(ctx, a.broker, CallSpec{
			API:         "bitable.v1.app_table_record.batch_update",
			Class:       CallClassBitable,
			Priority:    CallPriorityBackground,
			ResourceKey: bitableResourceKey(appToken, tableID),
			Retry:       RetryOff,
			Permission:  PermissionCooldownOnly,
		}, func(callCtx context.Context, client *lark.Client) (*larkbitable.BatchUpdateAppTableRecordResp, error) {
			resp, err := client.Bitable.V1.AppTableRecord.BatchUpdate(callCtx, larkbitable.NewBatchUpdateAppTableRecordReqBuilder().
				AppToken(appToken).
				TableId(tableID).
				Body(larkbitable.NewBatchUpdateAppTableRecordReqBodyBuilder().
					Records(items).
					Build()).
				Build())
			if err != nil {
				return resp, err
			}
			return resp, nil
		})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "drive.v1.permission_member.list",
		Class:       CallClassBitable,
		Priority:    CallPriorityReadAssist,
		ResourceKey: driveDocResourceKey(token),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.ListPermissionMemberResp, error) {
		resp, err := client.Drive.V1.PermissionMember.List(callCtx, larkdrive.NewListPermissionMemberReqBuilder().
			Token(token).
			Type(docType).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "drive.v1.permission_member.create",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: driveDocResourceKey(token),
		Retry:       RetryOff,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.CreatePermissionMemberResp, error) {
		resp, err := client.Drive.V1.PermissionMember.Create(callCtx, larkdrive.NewCreatePermissionMemberReqBuilder().
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
			return resp, err
		}
		return resp, nil
	})
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
	resp, err := DoSDK(ctx, a.broker, CallSpec{
		API:         "drive.v1.permission_member.update",
		Class:       CallClassBitable,
		Priority:    CallPriorityBackground,
		ResourceKey: driveDocResourceKey(token),
		Retry:       RetrySafe,
		Permission:  PermissionCooldownOnly,
	}, func(callCtx context.Context, client *lark.Client) (*larkdrive.UpdatePermissionMemberResp, error) {
		resp, err := client.Drive.V1.PermissionMember.Update(callCtx, larkdrive.NewUpdatePermissionMemberReqBuilder().
			Token(token).
			Type(docType).
			MemberId(strings.TrimSpace(memberID)).
			BaseMember(body.Build()).
			Build())
		if err != nil {
			return resp, err
		}
		return resp, nil
	})
	if err != nil {
		return err
	}
	if !resp.Success() {
		return newAPIError("drive.v1.permission_member.update", resp.ApiResp, resp.CodeError)
	}
	return nil
}
