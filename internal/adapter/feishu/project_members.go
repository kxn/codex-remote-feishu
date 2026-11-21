package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ProjectChatMembersRequest selects users by open_id/user_id/union_id, or existing
// application bots by app_id (not bot open_id). The caller must already belong to
// the chat and satisfy its invitation policy. Calls for one chat must be serialized
// by the service; the broker limits rate, not whole-operation concurrency.
type ProjectChatMembersRequest struct {
	ChatID       string
	MemberIDType string
	IDs          []string
	SucceedType  int // Platform succeed_type: 0/1/2, preserved rather than inferred.
}

type ProjectChatMembersResult struct{ InvalidIDs, NotExistedIDs, PendingApprovalIDs []string }

func projectUserIDType(value string) bool {
	return value == "open_id" || value == "user_id" || value == "union_id"
}

func (a *ProjectChatAPI) AddChatMembers(ctx context.Context, req ProjectChatMembersRequest) (ProjectChatMembersResult, error) {
	if err := projectChatID(req.ChatID); err != nil {
		return ProjectChatMembersResult{}, err
	}
	max := 50
	if req.MemberIDType == "app_id" {
		max = 5
	} else if !projectUserIDType(req.MemberIDType) {
		return ProjectChatMembersResult{}, fmt.Errorf("unsupported member id type %q", req.MemberIDType)
	}
	if len(req.IDs) == 0 || len(req.IDs) > max {
		return ProjectChatMembersResult{}, fmt.Errorf("member count must be 1..%d", max)
	}
	for _, id := range req.IDs {
		if strings.TrimSpace(id) == "" {
			return ProjectChatMembersResult{}, fmt.Errorf("empty member id")
		}
	}
	if req.SucceedType < 0 || req.SucceedType > 2 {
		return ProjectChatMembersResult{}, fmt.Errorf("unsupported succeed_type")
	}
	const api = "im.v1.chat.members.create"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, req.ChatID, false), func(ctx context.Context, c *lark.Client) (*larkim.CreateChatMembersResp, error) {
		resp, err := c.Im.V1.ChatMembers.Create(ctx, larkim.NewCreateChatMembersReqBuilder().ChatId(req.ChatID).MemberIdType(req.MemberIDType).SucceedType(req.SucceedType).Body(&larkim.CreateChatMembersReqBody{IdList: req.IDs}).Build())
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return nil, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if err != nil {
		return ProjectChatMembersResult{}, err
	}
	return ProjectChatMembersResult{InvalidIDs: resp.Data.InvalidIdList, NotExistedIDs: resp.Data.NotExistedIdList, PendingApprovalIDs: resp.Data.PendingApprovalIdList}, nil
}

type ProjectChatUser struct{ ID, IDType, Name, TenantKey string }

type ProjectChatUsersPage struct {
	Users                    []ProjectChatUser
	HasMore                  bool
	PageToken                string
	MemberTotal              *int
	TriggerSecurityConfLimit bool
	SecurityConfLimit        *int
}

// ListChatUsersPage returns platform pagination and security-limit evidence.
// The SDK explicitly documents that this endpoint filters bots. An empty user
// list proves nothing about bot membership. Query each configured role through
// IsCallerInChat on its own credential-bound adapter instead.
func (a *ProjectChatAPI) ListChatUsersPage(ctx context.Context, chatID, userIDType, pageToken string, pageSize int) (ProjectChatUsersPage, error) {
	if err := projectChatID(chatID); err != nil {
		return ProjectChatUsersPage{}, err
	}
	if !projectUserIDType(userIDType) {
		return ProjectChatUsersPage{}, fmt.Errorf("unsupported user id type %q", userIDType)
	}
	if pageSize < 1 || pageSize > 100 {
		return ProjectChatUsersPage{}, fmt.Errorf("page size must be 1..100")
	}
	const api = "im.v1.chat.members.get"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, true), func(ctx context.Context, c *lark.Client) (*larkim.GetChatMembersResp, error) {
		builder := larkim.NewGetChatMembersReqBuilder().ChatId(chatID).MemberIdType(userIDType).PageSize(pageSize)
		if pageToken != "" {
			builder.PageToken(pageToken)
		}
		resp, err := c.Im.V1.ChatMembers.Get(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return nil, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || resp.Data.HasMore == nil {
			return nil, fmt.Errorf("%s returned missing pagination data", api)
		}
		return resp, nil
	})
	if err != nil {
		return ProjectChatUsersPage{}, err
	}
	data := resp.Data
	page := ProjectChatUsersPage{Users: make([]ProjectChatUser, 0, len(data.Items)), HasMore: *data.HasMore, PageToken: scopeStringValue(data.PageToken), MemberTotal: data.MemberTotal, SecurityConfLimit: data.SecurityConfLimit}
	if data.TriggerSecurityConfLimit != nil {
		page.TriggerSecurityConfLimit = *data.TriggerSecurityConfLimit
	}
	for _, member := range data.Items {
		if member == nil {
			continue
		}
		page.Users = append(page.Users, ProjectChatUser{ID: scopeStringValue(member.MemberId), IDType: scopeStringValue(member.MemberIdType), Name: scopeStringValue(member.Name), TenantKey: scopeStringValue(member.TenantKey)})
	}
	if page.HasMore && (page.PageToken == "" || page.PageToken == pageToken) {
		return page, fmt.Errorf("%s returned non-advancing page token", api)
	}
	return page, nil
}

// IsCallerInChat observes only the application bound to this adapter's broker.
// It cannot query an arbitrary app_id using another bot's credentials.
func (a *ProjectChatAPI) IsCallerInChat(ctx context.Context, chatID string) (bool, error) {
	if err := projectChatID(chatID); err != nil {
		return false, err
	}
	const api = "im.v1.chat.members.is_in_chat"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, true), func(ctx context.Context, c *lark.Client) (*larkim.IsInChatChatMembersResp, error) {
		resp, err := c.Im.V1.ChatMembers.IsInChat(ctx, larkim.NewIsInChatChatMembersReqBuilder().ChatId(chatID).Build())
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return nil, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil || resp.Data.IsInChat == nil {
			return nil, fmt.Errorf("%s returned missing is_in_chat", api)
		}
		return resp, nil
	})
	if err != nil {
		return false, err
	}
	return *resp.Data.IsInChat, nil
}
