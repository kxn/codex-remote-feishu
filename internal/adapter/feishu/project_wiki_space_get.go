package feishu

import (
	"context"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
)

type ProjectWikiSpace struct{ ID, Name, Description, RequestID string }

func (a *ProjectDocumentAPI) GetWikiSpace(ctx context.Context, id string) (ProjectWikiSpace, error) {
	if err := projectBareToken(id, "existing Wiki space ID"); err != nil {
		return ProjectWikiSpace{}, err
	}
	const api = "wiki.v2.space.get"
	response, err := DoSDK(ctx, a.broker, projectResourceSpec(api, id, true), func(callCtx context.Context, client *lark.Client) (*larkwiki.GetSpaceResp, error) {
		r, e := client.Wiki.V2.Space.Get(callCtx, larkwiki.NewGetSpaceReqBuilder().SpaceId(id).Build())
		if e != nil {
			return r, e
		}
		if r == nil {
			return nil, fmt.Errorf("%s returned nil", api)
		}
		if !r.Success() {
			return r, newAPIError(api, r.ApiResp, r.CodeError)
		}
		if r.Data == nil || r.Data.Space == nil {
			return r, fmt.Errorf("existing space metadata missing")
		}
		return r, nil
	})
	if err != nil {
		return ProjectWikiSpace{}, err
	}
	space := response.Data.Space
	if scopeStringValue(space.SpaceId) != id {
		return ProjectWikiSpace{}, fmt.Errorf("different space identity returned")
	}
	return ProjectWikiSpace{ID: id, Name: scopeStringValue(space.Name), Description: scopeStringValue(space.Description), RequestID: response.RequestId()}, nil
}
