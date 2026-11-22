package feishu

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ProjectChatAPI uses one explicitly selected application's existing broker.
// The SDK endpoints also support user tokens, but this adapter uses tenant identity.
// It does not own entry bindings, reconcile human edits, retry uncertain writes,
// grant document permissions, reorder tabs, or clean up platform objects.
type ProjectChatAPI struct{ broker *FeishuCallBroker }

// ProjectChatAPI returns an adapter sharing this gateway’s selected credential
// and broker, so callers do not create a separate or global current-bot identity.
func (g *LiveGateway) ProjectChatAPI() *ProjectChatAPI {
	if g == nil {
		return nil
	}
	return NewProjectChatAPI(g.broker)
}

func NewProjectChatAPI(broker *FeishuCallBroker) *ProjectChatAPI {
	return &ProjectChatAPI{broker: broker}
}

type ProjectChatTab struct {
	ID       string
	Name     string
	Type     string // Reads retain system types; writes only accept doc and url.
	URL      string // Existing cloud document URL for doc; web URL for url.
	Embedded *bool  // Applies to url tabs; nil leaves the platform default unchanged.
}

func (a *ProjectChatAPI) callSpec(api, chatID string, read bool) CallSpec {
	spec := CallSpec{API: api, Class: CallClassIMPatch, Priority: CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{ReceiveTarget: chatID}, Retry: RetryOff, Permission: PermissionFailFast}
	if api == "im.v1.message.create" || api == "im.v1.message.reply" {
		spec.Class = CallClassIMSend
	}
	if read {
		spec.Class = CallClassIMRead
		spec.Priority = CallPriorityReadAssist
		spec.Retry = RetrySafe
	}
	return spec
}

func projectChatID(chatID string) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("missing chat id")
	}
	return nil
}

func projectSDKTab(tab ProjectChatTab, update bool) (*larkim.ChatTab, error) {
	if update && strings.TrimSpace(tab.ID) == "" {
		return nil, fmt.Errorf("missing tab id")
	}
	if !update && tab.ID != "" {
		return nil, fmt.Errorf("create tab must not supply tab id")
	}
	if strings.TrimSpace(tab.Name) == "" {
		return nil, fmt.Errorf("missing tab name")
	}
	if tab.Type != "doc" && tab.Type != "url" {
		return nil, fmt.Errorf("unsupported writable tab type %q", tab.Type)
	}
	parsed, err := url.Parse(tab.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("invalid tab URL")
	}
	content := &larkim.ChatTabContent{}
	if tab.Type == "doc" {
		content.Doc = &tab.URL
	} else {
		content.Url = &tab.URL
	}
	result := &larkim.ChatTab{TabName: &tab.Name, TabType: &tab.Type, TabContent: content}
	if update {
		result.TabId = &tab.ID
	}
	if tab.Embedded != nil {
		if tab.Type != "url" {
			return nil, fmt.Errorf("embedded setting requires url tab")
		}
		result.TabConfig = &larkim.ChatTabConfig{IsBuiltIn: tab.Embedded}
	}
	return result, nil
}

func projectTabs(tabs []*larkim.ChatTab) []ProjectChatTab {
	result := make([]ProjectChatTab, 0, len(tabs))
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		item := ProjectChatTab{ID: scopeStringValue(tab.TabId), Name: scopeStringValue(tab.TabName), Type: scopeStringValue(tab.TabType)}
		if tab.TabContent != nil {
			switch item.Type {
			case "doc":
				item.URL = scopeStringValue(tab.TabContent.Doc)
			case "url":
				item.URL = scopeStringValue(tab.TabContent.Url)
			}
		}
		if tab.TabConfig != nil {
			item.Embedded = tab.TabConfig.IsBuiltIn
		}
		result = append(result, item)
	}
	return result
}

// ListChatTabs is not paginated by the platform. Unknown/system tab types are
// returned for observation only and must not be claimed by title alone.
func (a *ProjectChatAPI) ListChatTabs(ctx context.Context, chatID string) ([]ProjectChatTab, error) {
	if err := projectChatID(chatID); err != nil {
		return nil, err
	}
	const api = "im.v1.chat.tab.list_tabs"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, true), func(ctx context.Context, c *lark.Client) (*larkim.ListTabsChatTabResp, error) {
		resp, err := c.Im.V1.ChatTab.ListTabs(ctx, larkim.NewListTabsChatTabReqBuilder().ChatId(chatID).Build())
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
		return nil, err
	}
	return projectTabs(resp.Data.ChatTabs), nil
}

func (a *ProjectChatAPI) CreateChatTab(ctx context.Context, chatID string, tab ProjectChatTab) ([]ProjectChatTab, error) {
	if err := projectChatID(chatID); err != nil {
		return nil, err
	}
	sdkTab, err := projectSDKTab(tab, false)
	if err != nil {
		return nil, err
	}
	const api = "im.v1.chat.tab.create"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, false), func(ctx context.Context, c *lark.Client) (*larkim.CreateChatTabResp, error) {
		resp, err := c.Im.V1.ChatTab.Create(ctx, larkim.NewCreateChatTabReqBuilder().ChatId(chatID).Body(&larkim.CreateChatTabReqBody{ChatTabs: []*larkim.ChatTab{sdkTab}}).Build())
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
		return nil, err
	}
	return projectTabs(resp.Data.ChatTabs), nil
}

func (a *ProjectChatAPI) UpdateChatTab(ctx context.Context, chatID string, tab ProjectChatTab) ([]ProjectChatTab, error) {
	if err := projectChatID(chatID); err != nil {
		return nil, err
	}
	sdkTab, err := projectSDKTab(tab, true)
	if err != nil {
		return nil, err
	}
	const api = "im.v1.chat.tab.update_tabs"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, false), func(ctx context.Context, c *lark.Client) (*larkim.UpdateTabsChatTabResp, error) {
		resp, err := c.Im.V1.ChatTab.UpdateTabs(ctx, larkim.NewUpdateTabsChatTabReqBuilder().ChatId(chatID).Body(&larkim.UpdateTabsChatTabReqBody{ChatTabs: []*larkim.ChatTab{sdkTab}}).Build())
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
		return nil, err
	}
	return projectTabs(resp.Data.ChatTabs), nil
}

// DeleteChatTab requires a specific external ID. Ownership checks belong to the service.
func (a *ProjectChatAPI) DeleteChatTab(ctx context.Context, chatID, tabID string) ([]ProjectChatTab, error) {
	if err := projectChatID(chatID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tabID) == "" {
		return nil, fmt.Errorf("missing tab id")
	}
	const api = "im.v1.chat.tab.delete_tabs"
	resp, err := DoSDK(ctx, a.broker, a.callSpec(api, chatID, false), func(ctx context.Context, c *lark.Client) (*larkim.DeleteTabsChatTabResp, error) {
		resp, err := c.Im.V1.ChatTab.DeleteTabs(ctx, larkim.NewDeleteTabsChatTabReqBuilder().ChatId(chatID).Body(&larkim.DeleteTabsChatTabReqBody{TabIds: []string{tabID}}).Build())
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
		return nil, err
	}
	return projectTabs(resp.Data.ChatTabs), nil
}
