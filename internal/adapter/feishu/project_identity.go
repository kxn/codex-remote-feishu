package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
)

// ResolveContactUserID returns only a tenant-local user ID from the actual
// gateway's contact API, and checks the returned open ID against the query.
func (a *ProjectChatAPI) ResolveContactUserID(ctx context.Context, openID string) (string, error) {
	if strings.TrimSpace(openID) == "" {
		return "", fmt.Errorf("open ID required")
	}
	const api = "contact.v3.user.get"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, "", true), func(ctx context.Context, client *lark.Client) (*larkcontact.GetUserResp, error) {
		r, e := client.Contact.V3.User.Get(ctx, larkcontact.NewGetUserReqBuilder().UserId(openID).UserIdType("open_id").Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		return r, nil
	})
	if err != nil {
		return "", err
	}
	if resp.Data == nil || resp.Data.User == nil || scopeStringValue(resp.Data.User.OpenId) != openID || strings.TrimSpace(scopeStringValue(resp.Data.User.UserId)) == "" {
		return "", fmt.Errorf("contact identity not verified")
	}
	return scopeStringValue(resp.Data.User.UserId), nil
}
