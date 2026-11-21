package workprovision

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type GatewayResolver func(context.Context, work.RoleBotBinding) (*feishu.LiveGateway, error)

func (f GatewayResolver) Resolve(ctx context.Context, binding work.RoleBotBinding) (Platform, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	gateway, err := f(ctx, binding)
	if err != nil {
		return nil, err
	}
	if gateway == nil {
		return nil, fmt.Errorf("selected bot gateway is unavailable")
	}
	gatewayID, appID := gateway.ProjectGatewayIdentity()
	if gatewayID != binding.GatewayRef || appID != binding.AppID {
		return nil, fmt.Errorf("resolved gateway credential does not match the frozen role binding")
	}
	return &FeishuPlatform{binding: binding, chat: gateway.ProjectChatAPI(), documents: gateway.ProjectDocumentAPI(), drive: gateway.ProjectBitableAPI()}, nil
}

type FeishuPlatform struct {
	binding   work.RoleBotBinding
	chat      *feishu.ProjectChatAPI
	documents *feishu.ProjectDocumentAPI
	drive     feishu.BitableAPI
}

func (p *FeishuPlatform) UsesFixedNavigationCard() {}

func (p *FeishuPlatform) Binding() work.RoleBotBinding { return p.binding }
func (p *FeishuPlatform) ConnectorIdentity() string {
	return workservice.FeishuConnectorIdentity(p.binding)
}
func (p *FeishuPlatform) IsCallerInChat(ctx context.Context, chat string) (bool, error) {
	return p.chat.IsCallerInChat(ctx, chat)
}
func (p *FeishuPlatform) InviteBots(ctx context.Context, chat string, ids []string) (feishu.ProjectChatMembersResult, error) {
	return p.chat.AddChatMembers(ctx, feishu.ProjectChatMembersRequest{ChatID: chat, MemberIDType: "app_id", IDs: ids, SucceedType: 1})
}
func (p *FeishuPlatform) CreateFolder(ctx context.Context, name, parent string) (Folder, error) {
	r, e := p.documents.CreateProjectFolder(ctx, name, parent)
	return Folder{ID: r.Token, URL: r.URL, RequestID: r.RequestID}, e
}
func (p *FeishuPlatform) CheckFolder(ctx context.Context, id string) error {
	return p.documents.CheckProjectFolder(ctx, id)
}
func (p *FeishuPlatform) CreateDocument(ctx context.Context, title, folder string) (HomeDocument, error) {
	r, e := p.documents.CreateDocument(ctx, feishu.ProjectCreateDocumentRequest{Title: title, FolderToken: folder})
	doc := HomeDocument{ID: string(r.DocumentID), RequestID: r.RequestID}
	if r.RevisionID != nil {
		doc.Revision = *r.RevisionID
	}
	return doc, e
}
func (p *FeishuPlatform) ReadDocument(ctx context.Context, id string) (HomeDocument, error) {
	r, e := p.documents.GetDocument(ctx, feishu.ProjectDocumentID(id))
	doc := HomeDocument{ID: string(r.DocumentID), RequestID: r.RequestID}
	if r.RevisionID != nil {
		doc.Revision = *r.RevisionID
	}
	return doc, e
}
func (p *FeishuPlatform) ReadDocumentURL(ctx context.Context, id string) (string, error) {
	return p.documents.ProjectFileURL(ctx, id, "docx")
}
func (p *FeishuPlatform) ResolveWikiParent(ctx context.Context, value string) (WikiParent, error) {
	if strings.HasPrefix(value, "wiki-space:") {
		id := strings.TrimPrefix(value, "wiki-space:")
		if id == "" {
			return WikiParent{}, fmt.Errorf("existing Wiki space ID required")
		}
		return WikiParent{SpaceID: id, AtSpaceRoot: true}, nil
	}
	if strings.HasPrefix(value, "wiki:") {
		parts := strings.SplitN(strings.TrimPrefix(value, "wiki:"), ":", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return WikiParent{SpaceID: parts[0], NodeToken: parts[1]}, nil
		}
		return WikiParent{}, fmt.Errorf("wiki container requires space and parent node")
	}
	link, err := url.Parse(value)
	if err != nil || link.Scheme != "https" {
		return WikiParent{}, fmt.Errorf("selected Wiki parent URL is invalid")
	}
	parts := strings.Split(strings.Trim(link.Path, "/"), "/")
	if len(parts) == 3 && parts[0] == "wiki" && parts[1] == "space" {
		space, err := p.documents.GetWikiSpace(ctx, parts[2])
		if err != nil {
			return WikiParent{}, err
		}
		return WikiParent{SpaceID: space.ID, AtSpaceRoot: true}, nil
	}
	if len(parts) != 2 || parts[0] != "wiki" {
		return WikiParent{}, fmt.Errorf("selected Wiki URL must name an existing node")
	}
	node, err := p.documents.ResolveWikiNode(ctx, feishu.ProjectWikiNodeToken(parts[1]))
	if err != nil {
		return WikiParent{}, err
	}
	return WikiParent{SpaceID: node.SpaceID, NodeToken: string(node.NodeToken)}, nil
}
func (p *FeishuPlatform) CreateWikiDocument(ctx context.Context, parent WikiParent, title string) (HomeDocument, error) {
	r, e := p.documents.CreateWikiNode(ctx, feishu.ProjectCreateWikiNodeRequest{SpaceID: parent.SpaceID, ParentNodeToken: feishu.ProjectWikiNodeToken(parent.NodeToken), AtSpaceRoot: parent.AtSpaceRoot, ObjType: "docx", Title: title})
	doc := HomeDocument{ID: r.ObjToken, URL: r.URL, NodeToken: string(r.NodeToken), RequestID: r.RequestID}
	return doc, e
}
func (p *FeishuPlatform) HasGroupView(ctx context.Context, id, chat string) (bool, error) {
	members, err := p.drive.ListPermissionMembers(ctx, id, "docx")
	if err != nil {
		return false, err
	}
	member, ok := members["openchat:"+chat]
	return ok && (member.Perm == "view" || member.Perm == "edit" || member.Perm == "full_access"), nil
}
func (p *FeishuPlatform) EnsureGroupView(ctx context.Context, id, chat string) error {
	return p.drive.GrantPermission(ctx, id, "docx", "openchat", chat, "chat", "view")
}
func (p *FeishuPlatform) ListTabs(ctx context.Context, chat string) ([]feishu.ProjectChatTab, error) {
	return p.chat.ListChatTabs(ctx, chat)
}
func (p *FeishuPlatform) CreateDocTab(ctx context.Context, chat, link string) (feishu.ProjectChatTab, error) {
	tabs, err := p.chat.CreateChatTab(ctx, chat, feishu.ProjectChatTab{Name: "项目", Type: "doc", URL: link})
	for _, tab := range tabs {
		if tab.ID != "" && tab.URL == link && (tab.Type == "doc" || tab.Type == "url") {
			return tab, err
		}
	}
	if err == nil {
		err = fmt.Errorf("platform returned no created tab ID")
	}
	return feishu.ProjectChatTab{}, err
}
func (p *FeishuPlatform) PublishNavigation(ctx context.Context, chat string, navigation Navigation, uuid string) (string, error) {
	// This is SDK plain text, not Markdown: all dynamic values remain literal.
	text := renderNavigation(navigation)
	receipt, err := p.chat.PublishText(ctx, feishu.ProjectPublishRequest{ChatID: chat, Text: text, UUID: uuid})
	return receipt.MessageID, err
}
func (p *FeishuPlatform) PinNavigation(ctx context.Context, messageID string) (string, error) {
	receipt, err := p.chat.PinMessage(ctx, messageID)
	return receipt.MessageID, err
}

func renderNavigation(navigation Navigation) string {
	text := "项目：" + navigation.ProjectName + "\n"
	if navigation.HomeURL != "" {
		text += "项目首页：" + navigation.HomeURL + "\n"
	}
	return text + "索引状态：" + navigation.SyncState
}
func (p *FeishuPlatform) ReadNavigation(ctx context.Context, id string) (feishu.ProjectNavigationMessage, error) {
	return p.chat.ReadProjectNavigation(ctx, id)
}
func (p *FeishuPlatform) HasNavigationPin(ctx context.Context, chatID, messageID string) (bool, error) {
	return p.chat.HasProjectPin(ctx, chatID, messageID)
}

// selectedFolderToken accepts a concrete folder identity, never a title search.
func selectedFolderToken(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	token := strings.TrimPrefix(value, "drive:")
	if strings.Contains(token, "://") {
		link, err := url.Parse(token)
		if err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil {
			return "", fmt.Errorf("selected Drive folder URL is invalid")
		}
		parts := strings.Split(strings.Trim(link.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "drive" || parts[1] != "folder" {
			return "", fmt.Errorf("selected URL must identify an existing Drive folder")
		}
		token = parts[2]
	}
	if token == "" || strings.ContainsAny(token, "/?#: \t\r\n") {
		return "", fmt.Errorf("selected folder token is invalid")
	}
	return token, nil
}
