package feishu

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type fakeCreateFolderCall struct {
	Name        string
	ParentToken string
}

type fakeUploadFileCall struct {
	ParentToken string
	FileName    string
	Content     string
}

type fakeQueryMetaCall struct {
	Token   string
	DocType string
}

type fakeGrantPermissionCall struct {
	Token     string
	DocType   string
	Principal previewPrincipal
}

type fakePreviewAPI struct {
	createFolderCalls    []fakeCreateFolderCall
	uploadFileCalls      []fakeUploadFileCall
	queryMetaURLCalls    []fakeQueryMetaCall
	grantPermissionCalls []fakeGrantPermissionCall

	createFolderFunc    func(context.Context, string, string) (previewRemoteNode, error)
	uploadFileFunc      func(context.Context, string, string, []byte) (string, error)
	queryMetaURLFunc    func(context.Context, string, string) (string, error)
	grantPermissionFunc func(context.Context, string, string, previewPrincipal) error

	nextFolder int
	nextFile   int
}

func newFakePreviewAPI() *fakePreviewAPI {
	return &fakePreviewAPI{}
}

func (f *fakePreviewAPI) CreateFolder(ctx context.Context, name, parentToken string) (previewRemoteNode, error) {
	f.createFolderCalls = append(f.createFolderCalls, fakeCreateFolderCall{Name: name, ParentToken: parentToken})
	if f.createFolderFunc != nil {
		return f.createFolderFunc(ctx, name, parentToken)
	}
	f.nextFolder++
	token := "folder-" + string(rune('0'+f.nextFolder))
	return previewRemoteNode{
		Token: token,
		URL:   "https://preview/" + token,
	}, nil
}

func (f *fakePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName string, content []byte) (string, error) {
	f.uploadFileCalls = append(f.uploadFileCalls, fakeUploadFileCall{
		ParentToken: parentToken,
		FileName:    fileName,
		Content:     string(content),
	})
	if f.uploadFileFunc != nil {
		return f.uploadFileFunc(ctx, parentToken, fileName, content)
	}
	f.nextFile++
	return "file-" + string(rune('0'+f.nextFile)), nil
}

func (f *fakePreviewAPI) QueryMetaURL(ctx context.Context, token, docType string) (string, error) {
	f.queryMetaURLCalls = append(f.queryMetaURLCalls, fakeQueryMetaCall{Token: token, DocType: docType})
	if f.queryMetaURLFunc != nil {
		return f.queryMetaURLFunc(ctx, token, docType)
	}
	return "https://preview/" + token, nil
}

func (f *fakePreviewAPI) GrantPermission(ctx context.Context, token, docType string, principal previewPrincipal) error {
	f.grantPermissionCalls = append(f.grantPermissionCalls, fakeGrantPermissionCall{
		Token:     token,
		DocType:   docType,
		Principal: principal,
	})
	if f.grantPermissionFunc != nil {
		return f.grantPermissionFunc(ctx, token, docType, principal)
	}
	return nil
}

func TestDriveMarkdownPreviewerPersistsCacheAndReusesUpload(t *testing.T) {
	root := t.TempDir()
	docPath := writeMarkdownFile(t, filepath.Join(root, "docs", "design.md"), "# design\n")
	statePath := filepath.Join(root, "state", "preview.json")

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "See [design](docs/design.md).",
		},
	}

	api1 := newFakePreviewAPI()
	previewer1 := NewDriveMarkdownPreviewer(api1, MarkdownPreviewConfig{
		StatePath:  statePath,
		ProcessCWD: root,
	})
	block1, err := previewer1.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if want := "See [design](https://preview/file-1)."; block1.Text != want {
		t.Fatalf("unexpected rewritten text: %q", block1.Text)
	}
	if len(api1.createFolderCalls) != 2 {
		t.Fatalf("expected root + scope folder creation, got %#v", api1.createFolderCalls)
	}
	if len(api1.uploadFileCalls) != 1 {
		t.Fatalf("expected one upload, got %#v", api1.uploadFileCalls)
	}
	if api1.uploadFileCalls[0].ParentToken != "folder-2" {
		t.Fatalf("expected upload into scope folder, got %#v", api1.uploadFileCalls[0])
	}
	if !strings.HasPrefix(api1.uploadFileCalls[0].FileName, "design--") || !strings.HasSuffix(api1.uploadFileCalls[0].FileName, ".md") {
		t.Fatalf("unexpected uploaded file name: %#v", api1.uploadFileCalls[0])
	}
	if api1.uploadFileCalls[0].Content != "# design\n" {
		t.Fatalf("unexpected uploaded content: %#v", api1.uploadFileCalls[0])
	}
	if len(api1.grantPermissionCalls) != 2 {
		t.Fatalf("expected folder + file grants, got %#v", api1.grantPermissionCalls)
	}
	if api1.grantPermissionCalls[0].Token != "folder-2" || api1.grantPermissionCalls[1].Token != "file-1" {
		t.Fatalf("unexpected grant targets: %#v", api1.grantPermissionCalls)
	}
	if api1.grantPermissionCalls[0].Principal.Key != "openid:ou_user" || api1.grantPermissionCalls[1].Principal.Key != "openid:ou_user" {
		t.Fatalf("unexpected grant principals: %#v", api1.grantPermissionCalls)
	}

	api2 := newFakePreviewAPI()
	previewer2 := NewDriveMarkdownPreviewer(api2, MarkdownPreviewConfig{
		StatePath:  statePath,
		ProcessCWD: root,
	})
	block2, err := previewer2.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("cached rewrite returned error: %v", err)
	}
	if block2.Text != block1.Text {
		t.Fatalf("expected same rewritten text from persisted cache, got %q vs %q", block2.Text, block1.Text)
	}
	if len(api2.createFolderCalls) != 0 || len(api2.uploadFileCalls) != 0 || len(api2.queryMetaURLCalls) != 0 || len(api2.grantPermissionCalls) != 0 {
		t.Fatalf("expected persisted cache to avoid remote calls, got create=%#v upload=%#v meta=%#v grant=%#v",
			api2.createFolderCalls, api2.uploadFileCalls, api2.queryMetaURLCalls, api2.grantPermissionCalls)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read preview state: %v", err)
	}
	if !strings.Contains(string(raw), docPath) {
		t.Fatalf("expected state file to contain source path %q, got %s", docPath, string(raw))
	}
}

func TestDriveMarkdownPreviewerUploadsNewVersionWhenMarkdownChanges(t *testing.T) {
	root := t.TempDir()
	docPath := writeMarkdownFile(t, filepath.Join(root, "docs", "design.md"), "# v1\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "state", "preview.json"),
		ProcessCWD: root,
	})

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Read [design](docs/design.md).",
		},
	}

	first, err := previewer.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if first.Text != "Read [design](https://preview/file-1)." {
		t.Fatalf("unexpected first rewrite: %q", first.Text)
	}

	writeMarkdownFile(t, docPath, "# v2\n")
	second, err := previewer.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("second rewrite returned error: %v", err)
	}
	if second.Text != "Read [design](https://preview/file-2)." {
		t.Fatalf("expected a new uploaded preview url after content change, got %q", second.Text)
	}
	if len(api.uploadFileCalls) != 2 {
		t.Fatalf("expected two uploads for two content hashes, got %#v", api.uploadFileCalls)
	}
	if api.uploadFileCalls[0].Content == api.uploadFileCalls[1].Content {
		t.Fatalf("expected uploaded content to change, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerCreatesGroupAndActorPermissions(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "plan.md"), "# plan\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	block, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:chat:oc_chat",
		ChatID:           "oc_chat",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [plan](docs/plan.md).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if block.Text != "Open [plan](https://preview/file-1)." {
		t.Fatalf("unexpected rewritten text: %q", block.Text)
	}

	wantKeys := map[string]bool{
		"folder-2|openid:ou_user":   true,
		"folder-2|openchat:oc_chat": true,
		"file-1|openid:ou_user":     true,
		"file-1|openchat:oc_chat":   true,
	}
	if len(api.grantPermissionCalls) != len(wantKeys) {
		t.Fatalf("unexpected grant calls: %#v", api.grantPermissionCalls)
	}
	for _, call := range api.grantPermissionCalls {
		key := call.Token + "|" + call.Principal.Key
		if !wantKeys[key] {
			t.Fatalf("unexpected grant call: %#v", call)
		}
		delete(wantKeys, key)
	}
	if len(wantKeys) != 0 {
		t.Fatalf("missing grant calls: %#v", wantKeys)
	}
}

func TestDriveMarkdownPreviewerRecreatesMissingScopeFolder(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "plan.md"), "# plan\n")
	statePath := filepath.Join(root, "state", "preview.json")

	initialState := &previewState{
		Root: &previewFolderRecord{Token: "fld-root", URL: "https://preview/fld-root"},
		Scopes: map[string]*previewScopeRecord{
			"feishu:user:ou_user": {
				Folder: &previewFolderRecord{
					Token: "fld-stale",
					URL:   "https://preview/fld-stale",
				},
			},
		},
		Files: map[string]*previewFileRecord{},
	}
	writePreviewState(t, statePath, initialState)

	api := newFakePreviewAPI()
	api.createFolderFunc = func(_ context.Context, name, parentToken string) (previewRemoteNode, error) {
		if parentToken != "fld-root" {
			t.Fatalf("expected stale scope folder to be recreated under root, got parent %q", parentToken)
		}
		return previewRemoteNode{Token: "fld-scope-new", URL: "https://preview/fld-scope-new"}, nil
	}
	api.grantPermissionFunc = func(_ context.Context, token, _ string, _ previewPrincipal) error {
		if token == "fld-stale" {
			return &driveAPIError{Code: 1063005, Msg: "resource deleted"}
		}
		return nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  statePath,
		ProcessCWD: root,
	})
	block, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [plan](docs/plan.md).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if block.Text != "Open [plan](https://preview/file-1)." {
		t.Fatalf("unexpected rewritten text: %q", block.Text)
	}
	if len(api.createFolderCalls) != 1 {
		t.Fatalf("expected only scope folder recreation, got %#v", api.createFolderCalls)
	}
	if api.uploadFileCalls[0].ParentToken != "fld-scope-new" {
		t.Fatalf("expected upload to use recreated scope folder, got %#v", api.uploadFileCalls[0])
	}
}

func TestDriveMarkdownPreviewerSkipsMarkdownOutsideAllowedRoots(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	secretPath := writeMarkdownFile(t, filepath.Join(other, "secret.md"), "# secret\n")

	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})
	block, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Ignore [secret](" + secretPath + ").",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if block.Text != "Ignore [secret]("+secretPath+")." {
		t.Fatalf("expected path outside roots to stay untouched, got %q", block.Text)
	}
	if len(api.createFolderCalls) != 0 || len(api.uploadFileCalls) != 0 || len(api.grantPermissionCalls) != 0 || len(api.queryMetaURLCalls) != 0 {
		t.Fatalf("expected no remote calls for outside path, got create=%#v upload=%#v meta=%#v grant=%#v",
			api.createFolderCalls, api.uploadFileCalls, api.queryMetaURLCalls, api.grantPermissionCalls)
	}
}

func TestPreviewScopeKeyIncludesGatewayID(t *testing.T) {
	got := previewScopeKey("app-1", "", "oc_chat", "")
	if got != "feishu:app-1:chat:oc_chat" {
		t.Fatalf("unexpected chat scope key: %q", got)
	}
	got = previewScopeKey("app-1", "", "", "ou_user")
	if got != "feishu:app-1:user:ou_user" {
		t.Fatalf("unexpected user scope key: %q", got)
	}
}

func TestPreviewPrincipalsRecognizeGatewayAwareChatSurface(t *testing.T) {
	principals := previewPrincipals("feishu:app-1:chat:oc_chat", "oc_chat", "ou_user")
	if len(principals) != 2 {
		t.Fatalf("expected user and chat principals, got %#v", principals)
	}
	if principals[0].Type != "user" || principals[1].Type != "chat" {
		t.Fatalf("unexpected principals order/types: %#v", principals)
	}
}

func writeMarkdownFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func writePreviewState(t *testing.T, path string, state *previewState) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir preview state dir: %v", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal preview state: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write preview state: %v", err)
	}
}
