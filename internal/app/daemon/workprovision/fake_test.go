package workprovision

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type fakeWorld struct {
	present     map[string]bool
	platforms   map[string]*fakePlatform
	selfChecks  []string
	inviteCalls [][]string
}
type fakePlatform struct {
	world                                                                                  *fakeWorld
	binding                                                                                work.RoleBotBinding
	doc                                                                                    HomeDocument
	blocks                                                                                 HomeBlocks
	tabs                                                                                   []feishu.ProjectChatTab
	messages                                                                               map[string]string
	pins                                                                                   map[string]bool
	shared                                                                                 bool
	folderCalls, documentCalls, appendCalls, updateCalls, publishCalls, tabCalls, pinCalls int
	createErr                                                                              error
	createHasID                                                                            bool
	afterCreate                                                                            func()
	appendErr                                                                              error
	inviteErr                                                                              error
	updateErr                                                                              error
	tabErr                                                                                 error
	tabHasID                                                                               bool
	pending                                                                                bool
}

func (p *fakePlatform) Binding() work.RoleBotBinding { return p.binding }
func (p *fakePlatform) ConnectorIdentity() string {
	return workservice.FeishuConnectorIdentity(p.binding)
}
func (p *fakePlatform) IsCallerInChat(context.Context, string) (bool, error) {
	p.world.selfChecks = append(p.world.selfChecks, p.binding.AppID)
	return p.world.present[p.binding.AppID], nil
}
func (p *fakePlatform) InviteBots(_ context.Context, _ string, ids []string) (feishu.ProjectChatMembersResult, error) {
	p.world.inviteCalls = append(p.world.inviteCalls, append([]string{}, ids...))
	if p.inviteErr != nil {
		return feishu.ProjectChatMembersResult{}, p.inviteErr
	}
	if p.pending {
		return feishu.ProjectChatMembersResult{PendingApprovalIDs: ids}, nil
	}
	for _, id := range ids {
		p.world.present[id] = true
	}
	return feishu.ProjectChatMembersResult{}, nil
}
func (p *fakePlatform) CreateFolder(context.Context, string, string) (Folder, error) {
	p.folderCalls++
	return Folder{ID: "folder-created", URL: "https://a.feishu.cn/drive/folder/folder-created"}, nil
}
func (p *fakePlatform) CheckFolder(context.Context, string) error { return nil }
func (p *fakePlatform) ResolveWikiParent(context.Context, string) (WikiParent, error) {
	return WikiParent{SpaceID: "existing-space", NodeToken: "existing-parent"}, nil
}
func (p *fakePlatform) CreateWikiDocument(ctx context.Context, _ WikiParent, title string) (HomeDocument, error) {
	return p.CreateDocument(ctx, title, "")
}
func (p *fakePlatform) CreateDocument(context.Context, string, string) (HomeDocument, error) {
	p.documentCalls++
	p.doc = HomeDocument{ID: "document-created", URL: "https://a.feishu.cn/docx/document-created", Revision: 1}
	if p.afterCreate != nil {
		p.afterCreate()
	}
	if p.createErr != nil && !p.createHasID {
		return HomeDocument{}, p.createErr
	}
	return p.doc, p.createErr
}
func (p *fakePlatform) ReadDocument(_ context.Context, id string) (HomeDocument, error) {
	if p.doc.ID != id {
		return HomeDocument{}, fmt.Errorf("document ID not found")
	}
	return p.doc, nil
}
func (p *fakePlatform) ReadDocumentURL(_ context.Context, id string) (string, error) {
	if p.doc.ID != id {
		return "", fmt.Errorf("document ID not found")
	}
	return p.doc.URL, nil
}
func fingerprint(id, text string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(id+"|"+text)))
}
func (p *fakePlatform) setBlocks(index HomeIndex) {
	text, status := renderIndex(index)
	p.doc.Revision++
	p.blocks = HomeBlocks{Index: HomeBlock{ID: "managed-index", Text: text, Fingerprint: fingerprint("managed-index", text)}, Status: HomeBlock{ID: "managed-status", Text: status, Fingerprint: fingerprint("managed-status", status)}, Revision: p.doc.Revision, RequestID: "read-" + strconv.Itoa(p.doc.Revision)}
}
func (p *fakePlatform) AppendHomeIndex(_ context.Context, id string, index HomeIndex, _ string) (HomeBlockIDs, error) {
	p.appendCalls++
	if p.appendErr != nil {
		return HomeBlockIDs{}, p.appendErr
	}
	if id != p.doc.ID {
		return HomeBlockIDs{}, fmt.Errorf("wrong document")
	}
	p.setBlocks(index)
	return HomeBlockIDs{IndexID: p.blocks.Index.ID, StatusID: p.blocks.Status.ID, Revision: p.doc.Revision}, nil
}
func (p *fakePlatform) ReadHomeBlocks(_ context.Context, id, indexID, statusID string) (HomeBlocks, error) {
	if id != p.doc.ID || indexID != p.blocks.Index.ID || statusID != p.blocks.Status.ID {
		return HomeBlocks{}, fmt.Errorf("managed block IDs not found")
	}
	return p.blocks, nil
}
func (p *fakePlatform) UpdateHomeIndex(_ context.Context, id, indexID, statusID string, index HomeIndex, _ string) (HomeBlocks, error) {
	p.updateCalls++
	if id != p.doc.ID || indexID != p.blocks.Index.ID || statusID != p.blocks.Status.ID {
		return HomeBlocks{}, fmt.Errorf("update changed ownership")
	}
	p.setBlocks(index)
	return p.blocks, p.updateErr
}
func (p *fakePlatform) EnsureGroupView(context.Context, string, string) error {
	p.shared = true
	return nil
}
func (p *fakePlatform) HasGroupView(context.Context, string, string) (bool, error) {
	return p.shared, nil
}
func (p *fakePlatform) ListTabs(context.Context, string) ([]feishu.ProjectChatTab, error) {
	return append([]feishu.ProjectChatTab{}, p.tabs...), nil
}
func (p *fakePlatform) CreateDocTab(_ context.Context, _ string, link string) (feishu.ProjectChatTab, error) {
	p.tabCalls++
	id := "system-project-tab"
	if p.tabCalls > 1 {
		id += "-" + strconv.Itoa(p.tabCalls)
	}
	tab := feishu.ProjectChatTab{ID: id, Name: "项目", Type: "doc", URL: link}
	p.tabs = append(p.tabs, tab)
	if p.tabErr != nil && !p.tabHasID {
		return feishu.ProjectChatTab{}, p.tabErr
	}
	return tab, p.tabErr
}
func (p *fakePlatform) PublishNavigation(_ context.Context, _ string, n Navigation, _ string) (string, error) {
	p.publishCalls++
	id := "navigation-message"
	p.messages[id] = renderNavigation(n)
	return id, nil
}
func (p *fakePlatform) ReadNavigation(_ context.Context, id string) (feishu.ProjectNavigationMessage, error) {
	body, ok := p.messages[id]
	if !ok {
		return feishu.ProjectNavigationMessage{}, fmt.Errorf("message not found")
	}
	return feishu.ProjectNavigationMessage{MessageID: id, ChatID: "project-chat", Text: body}, nil
}
func (p *fakePlatform) PinNavigation(_ context.Context, id string) (string, error) {
	p.pinCalls++
	p.pins[id] = true
	return id, nil
}
func (p *fakePlatform) HasNavigationPin(_ context.Context, _ string, id string) (bool, error) {
	return p.pins[id], nil
}
