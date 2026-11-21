package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workprovision"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workteamhome"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	"strings"
)

type teamMaterialsPlatform struct {
	workprovision.Platform
	gateway *feishu.LiveGateway
	binding work.RoleBotBinding
}

func (p teamMaterialsPlatform) ResolveHome(ctx context.Context, link string) (workprovision.HomeDocument, error) {
	connector, err := workservice.NewFeishuResourceConnector(workservice.FeishuConnectorIdentity(p.binding), p.gateway)
	if err != nil {
		return workprovision.HomeDocument{}, err
	}
	resolved, err := connector.Resolve(ctx, link)
	if err != nil {
		return workprovision.HomeDocument{}, err
	}
	if !strings.HasPrefix(resolved.SourceIdentity, "docx:") {
		return workprovision.HomeDocument{}, fmt.Errorf("团队首页必须是可读取的现有 Docx 或指向 Docx 的 Wiki 节点")
	}
	return workprovision.HomeDocument{ID: strings.TrimPrefix(resolved.SourceIdentity, "docx:"), URL: resolved.CanonicalURI}, nil
}
func (p teamMaterialsPlatform) InitializeHome(ctx context.Context, id, name, parent, token string) error {
	text := "团队共享资料：" + name + "\n\n这里维护团队共同使用的规范、方法和资料原件。各项目从自己的固定入口引用本页，不复制一份正文；归档某个项目不会删除这里的资料。项目特有的讨论、决定和个人聊天留在原范围。\n\n可以直接编辑本页，或告诉负责人要维护的具体文档。系统不会用项目状态刷新覆盖您的正文。"
	if parent != "" {
		text += "\n\n资料所在目录：" + parent
	}
	kind := 2
	sum := sha256.Sum256([]byte(token))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	token = fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
	result, err := p.gateway.ProjectDocumentAPI().AppendDocumentBlocks(ctx, feishu.ProjectAppendDocumentBlocksRequest{DocumentID: feishu.ProjectDocumentID(id), ParentBlockID: id, RevisionID: -1, Index: -1, ClientToken: token, Blocks: []*larkdocx.Block{{BlockType: &kind, Text: &larkdocx.Text{Elements: []*larkdocx.TextElement{{TextRun: &larkdocx.TextRun{Content: &text}}}}}}})
	if err == nil && len(result.Blocks) == 0 {
		return fmt.Errorf("平台未返回团队首页内容回执")
	}
	return err
}

// Shared team sources are collaborative documents. Only the team's owner may
// authorize the project audience; preserve stronger existing permissions.
func (p teamMaterialsPlatform) HasGroupView(ctx context.Context, id, chat string) (bool, error) {
	members, err := p.gateway.ProjectBitableAPI().ListPermissionMembers(ctx, id, "docx")
	if err != nil {
		return false, err
	}
	member, ok := members["openchat:"+chat]
	return ok && (member.Perm == "edit" || member.Perm == "full_access"), nil
}
func (p teamMaterialsPlatform) EnsureGroupView(ctx context.Context, id, chat string) error {
	return ensureTeamMaterialsEdit(ctx, p.gateway.ProjectBitableAPI(), id, chat)
}
func ensureTeamMaterialsEdit(ctx context.Context, api feishu.BitableAPI, id, chat string) error {
	members, err := api.ListPermissionMembers(ctx, id, "docx")
	if err != nil {
		return err
	}
	if member, ok := members["openchat:"+chat]; ok {
		if member.Perm == "edit" || member.Perm == "full_access" {
			return nil
		}
		return api.UpdatePermission(ctx, id, "docx", "openchat", chat, "chat", "edit", member.PermType)
	}
	return api.GrantPermission(ctx, id, "docx", "openchat", chat, "chat", "edit")
}
func (a *App) teamMaterialsResolver(ctx context.Context, binding work.RoleBotBinding) (workteamhome.Platform, error) {
	directory := teamDirectory{app: a, store: a.teamRuntime.store}
	gateway, err := directory.gateway(ctx, binding)
	if err != nil {
		return nil, err
	}
	base, err := workprovision.GatewayResolver(directory.gateway).Resolve(ctx, binding)
	if err != nil {
		return nil, err
	}
	return teamMaterialsPlatform{Platform: base, gateway: gateway, binding: binding}, nil
}
