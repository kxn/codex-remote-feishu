package feishu

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type fakeDeleteFileCall struct {
	Token   string
	DocType string
}

type fakeListFilesCall struct {
	FolderToken string
}

type fakeListPermissionMembersCall struct {
	Token   string
	DocType string
}

type fakePreviewAPI struct {
	createFolderCalls    []fakeCreateFolderCall
	uploadFileCalls      []fakeUploadFileCall
	queryMetaURLCalls    []fakeQueryMetaCall
	grantPermissionCalls []fakeGrantPermissionCall
	deleteFileCalls      []fakeDeleteFileCall
	listFilesCalls       []fakeListFilesCall
	listPermissionCalls  []fakeListPermissionMembersCall

	createFolderFunc    func(context.Context, string, string) (previewRemoteNode, error)
	uploadFileFunc      func(context.Context, string, string, []byte) (string, error)
	queryMetaURLFunc    func(context.Context, string, string) (string, error)
	grantPermissionFunc func(context.Context, string, string, previewPrincipal) error
	deleteFileFunc      func(context.Context, string, string) error
	listFilesFunc       func(context.Context, string) ([]previewRemoteNode, error)
	listPermissionFunc  func(context.Context, string, string) (map[string]bool, error)

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
		Type:  previewFolderType,
		Name:  name,
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

func (f *fakePreviewAPI) DeleteFile(ctx context.Context, token, docType string) error {
	f.deleteFileCalls = append(f.deleteFileCalls, fakeDeleteFileCall{Token: token, DocType: docType})
	if f.deleteFileFunc != nil {
		return f.deleteFileFunc(ctx, token, docType)
	}
	return nil
}

func (f *fakePreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
	f.listFilesCalls = append(f.listFilesCalls, fakeListFilesCall{FolderToken: folderToken})
	if f.listFilesFunc != nil {
		return f.listFilesFunc(ctx, folderToken)
	}
	return nil, nil
}

func (f *fakePreviewAPI) ListPermissionMembers(ctx context.Context, token, docType string) (map[string]bool, error) {
	f.listPermissionCalls = append(f.listPermissionCalls, fakeListPermissionMembersCall{Token: token, DocType: docType})
	if f.listPermissionFunc != nil {
		return f.listPermissionFunc(ctx, token, docType)
	}
	return map[string]bool{}, nil
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
	result1, err := previewer1.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if want := "See [design](https://preview/file-1)."; result1.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result1.Block.Text)
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
	result2, err := previewer2.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("cached rewrite returned error: %v", err)
	}
	if result2.Block.Text != result1.Block.Text {
		t.Fatalf("expected same rewritten text from persisted cache, got %q vs %q", result2.Block.Text, result1.Block.Text)
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

func TestDriveMarkdownPreviewerRewritesSingleFileHTMLLinks(t *testing.T) {
	root := t.TempDir()
	htmlPath := writePreviewFile(t, filepath.Join(root, "docs", "mock.html"), "<!doctype html><title>mock</title><h1>Mock</h1>")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "state", "preview.json"),
		ProcessCWD: root,
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [mock](docs/mock.html).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if result.Block.Text != "Open [mock](https://preview/file-1)." {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 {
		t.Fatalf("expected one upload, got %#v", api.uploadFileCalls)
	}
	if !strings.HasPrefix(api.uploadFileCalls[0].FileName, "mock--") || !strings.HasSuffix(api.uploadFileCalls[0].FileName, ".html") {
		t.Fatalf("unexpected uploaded file name: %#v", api.uploadFileCalls[0])
	}
	if api.uploadFileCalls[0].Content != "<!doctype html><title>mock</title><h1>Mock</h1>" {
		t.Fatalf("unexpected uploaded content: %#v", api.uploadFileCalls[0])
	}
	raw, err := os.ReadFile(previewer.config.StatePath)
	if err != nil {
		t.Fatalf("read preview state: %v", err)
	}
	if !strings.Contains(string(raw), htmlPath) {
		t.Fatalf("expected state file to contain source path %q, got %s", htmlPath, string(raw))
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

	firstResult, err := previewer.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if firstResult.Block.Text != "Read [design](https://preview/file-1)." {
		t.Fatalf("unexpected first rewrite: %q", firstResult.Block.Text)
	}

	writeMarkdownFile(t, docPath, "# v2\n")
	secondResult, err := previewer.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("second rewrite returned error: %v", err)
	}
	if secondResult.Block.Text != "Read [design](https://preview/file-2)." {
		t.Fatalf("expected a new uploaded preview url after content change, got %q", secondResult.Block.Text)
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

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
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
	if result.Block.Text != "Open [plan](https://preview/file-1)." {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
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

func TestDriveMarkdownPreviewerSummaryAndCleanupBefore(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	state := &previewState{
		Root: &previewFolderRecord{
			Token: "fld-root",
			URL:   "https://preview/fld-root",
		},
		Scopes: map[string]*previewScopeRecord{
			"feishu:app-1:chat:oc_chat": {
				Folder: &previewFolderRecord{Token: "fld-scope", URL: "https://preview/fld-scope"},
			},
		},
		Files: map[string]*previewFileRecord{
			"feishu:app-1:chat:oc_chat|/repo/docs/old.md|sha-old": {
				Path:       "/repo/docs/old.md",
				SHA256:     "sha-old",
				Token:      "file-old",
				URL:        "https://preview/file-old",
				ScopeKey:   "feishu:app-1:chat:oc_chat",
				SizeBytes:  10,
				CreatedAt:  now.Add(-72 * time.Hour),
				LastUsedAt: now.Add(-48 * time.Hour),
			},
			"feishu:app-1:chat:oc_chat|/repo/docs/recent.md|sha-recent": {
				Path:       "/repo/docs/recent.md",
				SHA256:     "sha-recent",
				Token:      "file-recent",
				URL:        "https://preview/file-recent",
				ScopeKey:   "feishu:app-1:chat:oc_chat",
				SizeBytes:  5,
				CreatedAt:  now.Add(-24 * time.Hour),
				LastUsedAt: now.Add(-2 * time.Hour),
			},
			"feishu:app-1:chat:oc_chat|/repo/docs/unknown.md|sha-unknown": {
				Path:   "/repo/docs/unknown.md",
				SHA256: "sha-unknown",
				Token:  "file-unknown",
				URL:    "https://preview/file-unknown",
			},
		},
	}

	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		deleted := map[string]bool{}
		for _, call := range api.deleteFileCalls {
			deleted[call.Token] = true
		}
		switch folderToken {
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-scope", Type: previewFolderType, Name: "feishu-app-1-chat-oc_chat"},
			}, nil
		case "fld-scope":
			files := []previewRemoteNode{
				{Token: "file-old", Type: previewFileType, Name: "old.md", CreatedTime: now.Add(-72 * time.Hour)},
				{Token: "file-recent", Type: previewFileType, Name: "recent.md", CreatedTime: now.Add(-24 * time.Hour)},
				{Token: "file-unknown", Type: previewFileType, Name: "unknown.md", CreatedTime: now.Add(-12 * time.Hour)},
			}
			filtered := make([]previewRemoteNode, 0, len(files))
			for _, file := range files {
				if !deleted[file.Token] {
					filtered = append(filtered, file)
				}
			}
			return filtered, nil
		default:
			return nil, nil
		}
	}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{StatePath: filepath.Join(t.TempDir(), "preview.json")})
	previewer.loaded = true
	previewer.state = normalizePreviewState(state)
	previewer.nowFn = func() time.Time { return now }

	summary, err := previewer.Summary()
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.FileCount != 3 || summary.ScopeCount != 1 || summary.EstimatedBytes != 15 || summary.UnknownSizeFileCount != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	result, err := previewer.CleanupBefore(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore returned error: %v", err)
	}
	if result.DeletedFileCount != 1 || result.DeletedEstimatedBytes != 10 || result.SkippedUnknownLastUsedCount != 1 {
		t.Fatalf("unexpected cleanup result: %#v", result)
	}
	if len(api.deleteFileCalls) != 1 || api.deleteFileCalls[0].Token != "file-old" || api.deleteFileCalls[0].DocType != previewFileType {
		t.Fatalf("unexpected delete calls: %#v", api.deleteFileCalls)
	}
	if result.Summary.FileCount != 2 || result.Summary.EstimatedBytes != 5 || result.Summary.UnknownSizeFileCount != 1 {
		t.Fatalf("unexpected post-cleanup summary: %#v", result.Summary)
	}
	if _, ok := previewer.state.Files["feishu:app-1:chat:oc_chat|/repo/docs/old.md|sha-old"]; ok {
		t.Fatalf("expected old preview file to be removed from state")
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
	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
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
	if result.Block.Text != "Open [plan](https://preview/file-1)." {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.createFolderCalls) != 1 {
		t.Fatalf("expected only scope folder recreation, got %#v", api.createFolderCalls)
	}
	if api.uploadFileCalls[0].ParentToken != "fld-scope-new" {
		t.Fatalf("expected upload to use recreated scope folder, got %#v", api.uploadFileCalls[0])
	}
}

func TestDriveMarkdownPreviewerCleanupBeforeDeletesManagedRemoteFilesWithoutLocalState(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "":
			return []previewRemoteNode{
				{
					Token:       "fld-root",
					Type:        previewFolderType,
					Name:        defaultPreviewRootFolderName,
					URL:         "https://preview/fld-root",
					CreatedTime: now.Add(-72 * time.Hour),
				},
			}, nil
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-scope", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-scope":
			return []previewRemoteNode{
				{
					Token:       "file-old",
					Type:        previewFileType,
					Name:        "old--deadbeef.md",
					CreatedTime: now.Add(-48 * time.Hour),
				},
			}, nil
		default:
			return nil, nil
		}
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		GatewayID: "main",
		StatePath: filepath.Join(t.TempDir(), "preview.json"),
	})
	previewer.nowFn = func() time.Time { return now }

	result, err := previewer.CleanupBefore(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore returned error: %v", err)
	}
	if result.DeletedFileCount != 1 {
		t.Fatalf("expected one remote-managed deletion, got %#v", result)
	}
	if len(api.deleteFileCalls) != 1 || api.deleteFileCalls[0].Token != "file-old" {
		t.Fatalf("unexpected delete calls: %#v", api.deleteFileCalls)
	}
	if previewer.state == nil || previewer.state.Root == nil || previewer.state.Root.Token != "fld-root" {
		t.Fatalf("expected discovered root to be cached in state, got %#v", previewer.state)
	}
	if previewer.state.LastCleanupAt != now {
		t.Fatalf("expected cleanup timestamp to be recorded, got %s", previewer.state.LastCleanupAt)
	}
}

func TestDriveMarkdownPreviewerCleanupBeforeDeletesNestedInventoryFiles(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "":
			return []previewRemoteNode{{
				Token:       "fld-root",
				Type:        previewFolderType,
				Name:        defaultPreviewRootFolderName,
				URL:         "https://preview/fld-root",
				CreatedTime: now.Add(-72 * time.Hour),
			}}, nil
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-scope", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-scope":
			return []previewRemoteNode{
				{Token: "fld-nested", Type: previewFolderType, Name: "nested"},
			}, nil
		case "fld-nested":
			return []previewRemoteNode{{
				Token:       "file-nested",
				Type:        previewFileType,
				Name:        "notes.md",
				CreatedTime: now.Add(-48 * time.Hour),
			}}, nil
		default:
			return nil, nil
		}
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath: filepath.Join(t.TempDir(), "preview.json"),
	})
	previewer.nowFn = func() time.Time { return now }

	result, err := previewer.CleanupBefore(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CleanupBefore returned error: %v", err)
	}
	if result.DeletedFileCount != 1 {
		t.Fatalf("expected one nested inventory deletion, got %#v", result)
	}
	if len(api.deleteFileCalls) != 1 || api.deleteFileCalls[0].Token != "file-nested" {
		t.Fatalf("unexpected delete calls: %#v", api.deleteFileCalls)
	}
}

func TestDriveMarkdownPreviewerSummaryUsesRemoteInventoryWithoutLocalRoot(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "":
			return []previewRemoteNode{{
				Token:       "fld-root",
				Type:        previewFolderType,
				Name:        defaultPreviewRootFolderName,
				URL:         "https://preview/fld-root",
				CreatedTime: now.Add(-72 * time.Hour),
			}}, nil
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-scope", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
				{
					Token:       "file-remote-only",
					Type:        previewFileType,
					Name:        "remote-only.md",
					CreatedTime: now.Add(-36 * time.Hour),
				},
			}, nil
		case "fld-scope":
			return []previewRemoteNode{{
				Token:       "file-tracked",
				Type:        previewFileType,
				Name:        "tracked.md",
				CreatedTime: now.Add(-48 * time.Hour),
			}}, nil
		default:
			return nil, nil
		}
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{StatePath: filepath.Join(t.TempDir(), "preview.json")})
	previewer.loaded = true
	previewer.state = normalizePreviewState(&previewState{
		Files: map[string]*previewFileRecord{
			"feishu:main:chat:oc_chat|/repo/docs/tracked.md|sha-tracked": {
				Token:      "file-tracked",
				ScopeKey:   "feishu:main:chat:oc_chat",
				SizeBytes:  42,
				LastUsedAt: now.Add(-2 * time.Hour),
			},
		},
	})

	summary, err := previewer.Summary()
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.RootToken != "fld-root" || summary.RootURL != "https://preview/fld-root" {
		t.Fatalf("expected remote root to be discovered, got %#v", summary)
	}
	if summary.FileCount != 2 || summary.ScopeCount != 1 {
		t.Fatalf("expected remote inventory counts, got %#v", summary)
	}
	if summary.EstimatedBytes != 42 || summary.UnknownSizeFileCount != 1 {
		t.Fatalf("expected tracked size cache plus one unknown remote file, got %#v", summary)
	}
	if previewer.state == nil || previewer.state.Root == nil || previewer.state.Root.Token != "fld-root" {
		t.Fatalf("expected discovered root to be cached in state, got %#v", previewer.state)
	}
}

func TestDriveMarkdownPreviewerRewriteDoesNotRunCleanup(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "one.md"), "# one\n")

	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "":
			return []previewRemoteNode{
				{
					Token:       "fld-root",
					Type:        previewFolderType,
					Name:        defaultPreviewRootFolderName,
					URL:         "https://preview/fld-root",
					CreatedTime: now.Add(-72 * time.Hour),
				},
			}, nil
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-old", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-old":
			return []previewRemoteNode{
				{
					Token:       "file-old",
					Type:        previewFileType,
					Name:        "__crp__legacy--deadbeef.md",
					CreatedTime: now.Add(-48 * time.Hour),
				},
			}, nil
		default:
			return nil, nil
		}
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		GatewayID:  "main",
		StatePath:  filepath.Join(root, "state", "preview.json"),
		ProcessCWD: root,
	})
	previewer.nowFn = func() time.Time { return now }

	req1 := MarkdownPreviewRequest{
		GatewayID:        "main",
		SurfaceSessionID: "feishu:main:chat:oc_chat",
		ChatID:           "oc_chat",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [one](docs/one.md).",
		},
	}
	result, err := previewer.RewriteFinalBlock(context.Background(), req1)
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if result.Block.Text != "Open [one](https://preview/file-1)." {
		t.Fatalf("unexpected rewrite: %q", result.Block.Text)
	}
	if len(api.deleteFileCalls) != 0 {
		t.Fatalf("expected rewrite upload path to skip cleanup, got %#v", api.deleteFileCalls)
	}
}

func TestDriveMarkdownPreviewerSummaryReturnsPermissionRequiredFallback(t *testing.T) {
	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		if folderToken == "" {
			return nil, &driveAPIError{Code: 99991672, Msg: "Access denied"}
		}
		return nil, nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{StatePath: filepath.Join(t.TempDir(), "preview.json")})
	summary, err := previewer.Summary()
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Status != "permission_required" {
		t.Fatalf("expected permission_required fallback, got %#v", summary)
	}
	if !strings.Contains(summary.StatusMessage, "drive:drive") {
		t.Fatalf("expected permission guidance, got %#v", summary)
	}
}

func TestDriveMarkdownPreviewerSummaryReturnsAPIUnavailableFallback(t *testing.T) {
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{StatePath: filepath.Join(t.TempDir(), "preview.json")})
	summary, err := previewer.Summary()
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Status != "api_unavailable" {
		t.Fatalf("expected api_unavailable fallback, got %#v", summary)
	}
}

func TestDriveMarkdownPreviewerBackgroundCleanupRunsOnInterval(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	deleted := make(chan string, 1)
	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "":
			return []previewRemoteNode{
				{
					Token:       "fld-root",
					Type:        previewFolderType,
					Name:        defaultPreviewRootFolderName,
					URL:         "https://preview/fld-root",
					CreatedTime: now.Add(-72 * time.Hour),
				},
			}, nil
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-old", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-old":
			return []previewRemoteNode{
				{
					Token:       "file-old",
					Type:        previewFileType,
					Name:        "__crp__legacy--deadbeef.md",
					CreatedTime: now.Add(-48 * time.Hour),
				},
			}, nil
		default:
			return nil, nil
		}
	}
	api.deleteFileFunc = func(_ context.Context, token, _ string) error {
		select {
		case deleted <- token:
		default:
		}
		return nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		GatewayID:               "main",
		StatePath:               filepath.Join(t.TempDir(), "preview.json"),
		BackgroundCleanupEvery:  20 * time.Millisecond,
		BackgroundCleanupMaxAge: 24 * time.Hour,
	})
	previewer.nowFn = time.Now

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		previewer.RunBackgroundMaintenance(ctx)
	}()

	select {
	case token := <-deleted:
		if token != "file-old" {
			t.Fatalf("unexpected deleted token: %q", token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background cleanup")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background cleanup goroutine to stop")
	}
	if previewer.state == nil || previewer.state.LastCleanupAt.IsZero() {
		t.Fatalf("expected background cleanup to record last cleanup timestamp, got %#v", previewer.state)
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
	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
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
	if result.Block.Text != "Ignore [secret]("+secretPath+")." {
		t.Fatalf("expected path outside roots to stay untouched, got %q", result.Block.Text)
	}
	if len(api.createFolderCalls) != 0 || len(api.uploadFileCalls) != 0 || len(api.grantPermissionCalls) != 0 || len(api.queryMetaURLCalls) != 0 {
		t.Fatalf("expected no remote calls for outside path, got create=%#v upload=%#v meta=%#v grant=%#v",
			api.createFolderCalls, api.uploadFileCalls, api.queryMetaURLCalls, api.grantPermissionCalls)
	}
}

func TestDriveMarkdownPreviewerLeavesUnhandledFileLinksUntouched(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "note.txt"), "plain text\n")

	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})
	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Keep [note](docs/note.txt).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if result.Block.Text != "Keep [note](docs/note.txt)." {
		t.Fatalf("expected unhandled file link to stay untouched, got %q", result.Block.Text)
	}
	if len(api.createFolderCalls) != 0 || len(api.uploadFileCalls) != 0 || len(api.grantPermissionCalls) != 0 || len(api.queryMetaURLCalls) != 0 {
		t.Fatalf("expected no remote calls for unhandled link, got create=%#v upload=%#v meta=%#v grant=%#v",
			api.createFolderCalls, api.uploadFileCalls, api.queryMetaURLCalls, api.grantPermissionCalls)
	}
}

func TestDriveMarkdownPreviewerSupportsRegisteredHandlerPublisherChain(t *testing.T) {
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{})
	previewer.handlers = nil
	previewer.publishers = nil
	previewer.RegisterHandler(testPreviewHandler{
		matchTarget: "docs/demo.custom",
		plan: &PreviewPlan{
			HandlerID: "custom_handler",
			Artifact: PreparedPreviewArtifact{
				ArtifactKind: "custom",
			},
			Deliveries: []PreviewDeliveryPlan{{
				Kind: PreviewDeliveryDriveFileLink,
			}},
		},
	})
	previewer.RegisterPublisher(testPreviewPublisher{
		supportedKind: "custom",
		result: &PreviewPublishResult{
			PublisherID: "custom_publisher",
			Mode:        PreviewPublishModeInlineLink,
			URL:         "https://preview/custom-link",
		},
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [demo](docs/demo.custom).",
		},
	})
	if err != nil {
		t.Fatalf("RewriteFinalBlock returned error: %v", err)
	}
	if result.Block.Text != "Open [demo](https://preview/custom-link)." {
		t.Fatalf("expected registered handler/publisher to rewrite link, got %q", result.Block.Text)
	}
}

func TestPreviewPathCandidatesTreatWindowsDrivePathAsAbsolute(t *testing.T) {
	roots := []string{`D:\Work\GoDot\interview-simulator`}
	target := `d:\Work\GoDot\interview-simulator\docs\characters.md`

	candidates := previewPathCandidates(target, roots)

	if len(candidates) != 1 || candidates[0] != target {
		t.Fatalf("expected absolute windows path to bypass root join, got %#v", candidates)
	}
}

type testPreviewHandler struct {
	matchTarget string
	plan        *PreviewPlan
}

func (h testPreviewHandler) ID() string { return "test_handler" }

func (h testPreviewHandler) Match(_ FinalBlockPreviewRequest, ref PreviewReference) bool {
	return ref.RawTarget == h.matchTarget
}

func (h testPreviewHandler) Plan(_ context.Context, _ FinalBlockPreviewRequest, _ PreviewReference) (*PreviewPlan, bool, error) {
	if h.plan == nil {
		return nil, false, nil
	}
	return h.plan, true, nil
}

type testPreviewPublisher struct {
	supportedKind string
	result        *PreviewPublishResult
}

func (p testPreviewPublisher) ID() string { return "test_publisher" }

func (p testPreviewPublisher) Supports(_ PreviewDeliveryPlan, artifact PreparedPreviewArtifact) bool {
	return artifact.ArtifactKind == p.supportedKind
}

func (p testPreviewPublisher) Publish(_ context.Context, _ PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	if p.result == nil {
		return nil, false, nil
	}
	return p.result, true, nil
}

func TestPreviewPathCandidatesTreatSlashPrefixedWindowsDrivePathAsAbsolute(t *testing.T) {
	roots := []string{`D:\Work\GoDot\interview-simulator`}
	target := `/d:/Work/GoDot/interview-simulator/docs/characters.md`

	candidates := previewPathCandidates(target, roots)

	if len(candidates) != 1 || candidates[0] != `d:/Work/GoDot/interview-simulator/docs/characters.md` {
		t.Fatalf("expected slash-prefixed windows path to normalize as absolute, got %#v", candidates)
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
	return writePreviewFile(t, path, content)
}

func writePreviewFile(t *testing.T, path, content string) string {
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
