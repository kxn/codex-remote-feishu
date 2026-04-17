package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
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
	mu                   sync.Mutex
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
	f.mu.Lock()
	f.createFolderCalls = append(f.createFolderCalls, fakeCreateFolderCall{Name: name, ParentToken: parentToken})
	createFolderFunc := f.createFolderFunc
	if createFolderFunc == nil {
		f.nextFolder++
	}
	nextFolder := f.nextFolder
	f.mu.Unlock()
	if createFolderFunc != nil {
		return createFolderFunc(ctx, name, parentToken)
	}
	token := "folder-" + string(rune('0'+nextFolder))
	return previewRemoteNode{
		Token: token,
		URL:   "https://preview/" + token,
		Type:  previewFolderType,
		Name:  name,
	}, nil
}

func (f *fakePreviewAPI) UploadFile(ctx context.Context, parentToken, fileName string, content []byte) (string, error) {
	f.mu.Lock()
	f.uploadFileCalls = append(f.uploadFileCalls, fakeUploadFileCall{
		ParentToken: parentToken,
		FileName:    fileName,
		Content:     string(content),
	})
	uploadFileFunc := f.uploadFileFunc
	if uploadFileFunc == nil {
		f.nextFile++
	}
	nextFile := f.nextFile
	f.mu.Unlock()
	if uploadFileFunc != nil {
		return uploadFileFunc(ctx, parentToken, fileName, content)
	}
	return "file-" + string(rune('0'+nextFile)), nil
}

func (f *fakePreviewAPI) QueryMetaURL(ctx context.Context, token, docType string) (string, error) {
	f.mu.Lock()
	f.queryMetaURLCalls = append(f.queryMetaURLCalls, fakeQueryMetaCall{Token: token, DocType: docType})
	queryMetaURLFunc := f.queryMetaURLFunc
	f.mu.Unlock()
	if queryMetaURLFunc != nil {
		return queryMetaURLFunc(ctx, token, docType)
	}
	return "https://preview/" + token, nil
}

func (f *fakePreviewAPI) GrantPermission(ctx context.Context, token, docType string, principal previewPrincipal) error {
	f.mu.Lock()
	f.grantPermissionCalls = append(f.grantPermissionCalls, fakeGrantPermissionCall{
		Token:     token,
		DocType:   docType,
		Principal: principal,
	})
	grantPermissionFunc := f.grantPermissionFunc
	f.mu.Unlock()
	if grantPermissionFunc != nil {
		return grantPermissionFunc(ctx, token, docType, principal)
	}
	return nil
}

func (f *fakePreviewAPI) DeleteFile(ctx context.Context, token, docType string) error {
	f.mu.Lock()
	f.deleteFileCalls = append(f.deleteFileCalls, fakeDeleteFileCall{Token: token, DocType: docType})
	deleteFileFunc := f.deleteFileFunc
	f.mu.Unlock()
	if deleteFileFunc != nil {
		return deleteFileFunc(ctx, token, docType)
	}
	return nil
}

func (f *fakePreviewAPI) ListFiles(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
	f.mu.Lock()
	f.listFilesCalls = append(f.listFilesCalls, fakeListFilesCall{FolderToken: folderToken})
	listFilesFunc := f.listFilesFunc
	f.mu.Unlock()
	if listFilesFunc != nil {
		return listFilesFunc(ctx, folderToken)
	}
	return nil, nil
}

func (f *fakePreviewAPI) ListPermissionMembers(ctx context.Context, token, docType string) (map[string]bool, error) {
	f.mu.Lock()
	f.listPermissionCalls = append(f.listPermissionCalls, fakeListPermissionMembersCall{Token: token, DocType: docType})
	listPermissionFunc := f.listPermissionFunc
	f.mu.Unlock()
	if listPermissionFunc != nil {
		return listPermissionFunc(ctx, token, docType)
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

	assertPreviewStateContainsSourcePath(t, statePath, docPath)
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
	assertPreviewStateContainsSourcePath(t, previewer.config.StatePath, htmlPath)
}

func assertPreviewStateContainsSourcePath(t *testing.T, statePath, wantPath string) {
	t.Helper()
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read preview state: %v", err)
	}
	var state previewState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode preview state: %v", err)
	}
	for _, file := range state.Files {
		if file != nil && testutil.SamePath(file.Path, wantPath) {
			return
		}
	}
	t.Fatalf("expected state file to contain source path %q, got %s", wantPath, string(raw))
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

func TestDriveMarkdownPreviewerRecoversStandaloneInlineCodeMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "inside.md"), "# inside\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "outside.md"), "# outside\n")
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
			Text:  "先看 `[inside](docs/inside.md)`，再打开 [outside](docs/outside.md)。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "先看 [inside](https://preview/file-1)，再打开 [outside](https://preview/file-2)。"
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 2 {
		t.Fatalf("expected inline and outside markdown links to be materialized, got %#v", api.uploadFileCalls)
	}
	if !strings.HasPrefix(api.uploadFileCalls[0].FileName, "inside--") || !strings.HasPrefix(api.uploadFileCalls[1].FileName, "outside--") {
		t.Fatalf("unexpected uploaded files: %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerSkipsCommandLikeInlineCodeReferences(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "inside.md"), "# inside\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "outside.md"), "# outside\n")
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
			Text:  "保留命令 `cat docs/inside.md | head -n 5`，再打开 [outside](docs/outside.md)。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "保留命令 `cat docs/inside.md | head -n 5`，再打开 [outside](https://preview/file-1)。"
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 || !strings.HasPrefix(api.uploadFileCalls[0].FileName, "outside--") {
		t.Fatalf("expected only outside markdown link to be materialized, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerSkipsFencedCodeMarkdownLinks(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "inside.md"), "# inside\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "outside.md"), "# outside\n")
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
			Text:  "```md\n[inside](docs/inside.md)\n```\n\n然后打开 [outside](docs/outside.md)。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "```md\n[inside](docs/inside.md)\n```\n\n然后打开 [outside](https://preview/file-1)。"
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 || !strings.HasPrefix(api.uploadFileCalls[0].FileName, "outside--") {
		t.Fatalf("expected only outside markdown link to be materialized, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerRewritesStandaloneBareFileReferences(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "plain.md"), "# plain\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "code.md"), "# code\n")
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
			Text:  "先看 docs/plain.md，再看 `docs/code.md`。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "先看 [docs/plain.md](https://preview/file-1)，再看 [docs/code.md](https://preview/file-2)。"
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 2 {
		t.Fatalf("expected both standalone references to be materialized, got %#v", api.uploadFileCalls)
	}
	if !strings.HasPrefix(api.uploadFileCalls[0].FileName, "plain--") || !strings.HasPrefix(api.uploadFileCalls[1].FileName, "code--") {
		t.Fatalf("unexpected uploaded files: %#v", api.uploadFileCalls)
	}
}

func TestParseStandalonePreviewReferenceAtStopsAtChinesePunctuation(t *testing.T) {
	text := "先看 docs/plain.md，再看"
	start := strings.Index(text, "docs/plain.md")
	if start < 0 {
		t.Fatalf("failed to locate candidate in %q", text)
	}
	end, rawTarget, display, ok := parseStandalonePreviewReferenceAt(text, start)
	if !ok {
		t.Fatalf("expected standalone preview reference match in %q", text)
	}
	if got := text[start:end]; got != "docs/plain.md" {
		t.Fatalf("unexpected matched text: %q", got)
	}
	if rawTarget != "docs/plain.md" || display != "docs/plain.md" {
		t.Fatalf("unexpected parsed target/display: raw=%q display=%q", rawTarget, display)
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
	phase := "summary"
	var (
		summaryCtx context.Context
		cleanupCtx context.Context
	)
	api.listFilesFunc = func(ctx context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch phase {
		case "summary":
			if summaryCtx == nil {
				summaryCtx = ctx
			}
		case "cleanup":
			if cleanupCtx == nil {
				cleanupCtx = ctx
			}
		}
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

	summary, err := previewer.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.FileCount != 3 || summary.ScopeCount != 1 || summary.EstimatedBytes != 15 || summary.UnknownSizeFileCount != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	assertPreviewContextHasDeadlineWithin(t, summaryCtx, previewDriveSummaryTimeout)

	phase = "cleanup"
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
	assertPreviewContextHasDeadlineWithin(t, cleanupCtx, previewDriveCleanupTimeout)
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

	summary, err := previewer.Summary(context.Background())
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
	summary, err := previewer.Summary(context.Background())
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
	summary, err := previewer.Summary(context.Background())
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
	var deleteCtx context.Context
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
	api.deleteFileFunc = func(ctx context.Context, token, _ string) error {
		deleteCtx = ctx
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
	assertPreviewContextHasDeadlineWithin(t, deleteCtx, previewDriveBackgroundCleanupTimeout)
}

func TestDriveMarkdownPreviewerBackgroundCleanupStillRunsWebCleanupWhenDriveFails(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC)

	previewer := NewDriveMarkdownPreviewer(newFakePreviewAPI(), MarkdownPreviewConfig{
		GatewayID:               "main",
		StatePath:               filepath.Join(root, "state", "preview.json"),
		CacheDir:                filepath.Join(root, "preview-cache"),
		ProcessCWD:              root,
		BackgroundCleanupEvery:  time.Minute,
		BackgroundCleanupMaxAge: 24 * time.Hour,
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})
	sourcePath := filepath.Join(root, "docs", "old.txt")
	_, expiredPreviewID := publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("old preview\n"), now.Add(-2*time.Hour))

	manifest, err := previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	expiredRecord := manifest.Records[expiredPreviewID]
	if expiredRecord == nil {
		t.Fatalf("missing expired record: %#v", manifest.Records)
	}
	expiredRecord.ExpiresAt = now.Add(-time.Minute)
	if err := previewer.saveWebPreviewScopeManifest(manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	blobPath := previewer.previewBlobPath(expiredRecord.BlobKey)
	if err := os.Chtimes(blobPath, now.Add(-defaultPreviewBlobTTL-time.Hour), now.Add(-defaultPreviewBlobTTL-time.Hour)); err != nil {
		t.Fatalf("age expired blob: %v", err)
	}

	api := newFakePreviewAPI()
	api.listFilesFunc = func(context.Context, string) ([]previewRemoteNode, error) {
		return nil, fmt.Errorf("drive unavailable")
	}
	previewer.api = api
	previewer.nowFn = func() time.Time { return now }

	err = previewer.runBackgroundCleanup(context.Background())
	if err == nil {
		t.Fatal("expected drive cleanup error")
	}
	if !strings.Contains(err.Error(), "drive cleanup failed") {
		t.Fatalf("expected drive cleanup error context, got %v", err)
	}

	manifest, err = previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if manifest != nil && len(manifest.Records) != 0 {
		t.Fatalf("expected web cleanup to prune expired record despite drive failure, got %#v", manifest.Records)
	}
	if _, err := os.Stat(blobPath); !os.IsNotExist(err) {
		t.Fatalf("expected web cleanup to remove expired blob despite drive failure, got err=%v", err)
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

type fakeWebPreviewPublisher struct {
	baseURL   string
	issuedFor []WebPreviewGrantRequest
	returnErr error
}

func (p *fakeWebPreviewPublisher) IssueScopePrefix(_ context.Context, req WebPreviewGrantRequest) (string, error) {
	p.issuedFor = append(p.issuedFor, req)
	if p.returnErr != nil {
		return "", p.returnErr
	}
	return p.baseURL, nil
}

func TestDriveMarkdownPreviewerRewritesTextFileToWebPreviewLink(t *testing.T) {
	root := t.TempDir()
	notePath := writePreviewFile(t, filepath.Join(root, "docs", "note.txt"), "plain text\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	web := &fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"}
	previewer.SetWebPreviewPublisher(web)

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [note](docs/note.txt).",
		},
	}
	result, err := previewer.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if !strings.HasPrefix(result.Block.Text, "Open [note](https://preview.example/g/shared/") || !strings.Contains(result.Block.Text, "?t=token).") {
		t.Fatalf("expected web preview link, got %q", result.Block.Text)
	}
	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	scopePublicID := previewScopePublicID(scopeKey)
	if len(web.issuedFor) != 1 || web.issuedFor[0].ScopePublicID != scopePublicID || strings.TrimSpace(web.issuedFor[0].GrantKey) == "" {
		t.Fatalf("unexpected issued requests: %#v want scope=%q grantKey!=empty", web.issuedFor, scopePublicID)
	}
	manifest, err := previewer.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest == nil || len(manifest.Records) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	record := firstWebPreviewRecord(manifest)
	canonicalNotePath, err := previewCanonicalPath(notePath)
	if err != nil {
		t.Fatalf("canonical note path: %v", err)
	}
	if record == nil || record.SourcePath != canonicalNotePath {
		t.Fatalf("unexpected record: %#v", record)
	}
	if _, err := os.Stat(previewer.previewBlobPath(record.BlobKey)); err != nil {
		t.Fatalf("expected preview blob to exist: %v", err)
	}
}

func TestDriveMarkdownPreviewerRewritesCodeFileLocationToWebPreviewLink(t *testing.T) {
	root := t.TempDir()
	writePreviewFile(t, filepath.Join(root, "internal", "main.go"), "package main\n\nfunc main() {}\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	web := &fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"}
	previewer.SetWebPreviewPublisher(web)

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open `internal/main.go:3`.",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if !strings.Contains(result.Block.Text, "[internal/main.go:3](https://preview.example/g/shared/") {
		t.Fatalf("expected web preview link for code file location, got %q", result.Block.Text)
	}
	if !strings.Contains(result.Block.Text, "loc=L3") || !strings.Contains(result.Block.Text, "#L3") {
		t.Fatalf("expected location to be carried into preview url, got %q", result.Block.Text)
	}
	if len(web.issuedFor) != 1 {
		t.Fatalf("expected one web preview grant request, got %#v", web.issuedFor)
	}
}

func TestDriveMarkdownPreviewerUsesSameGrantKeyForMultipleLinksInOneMessage(t *testing.T) {
	root := t.TempDir()
	writePreviewFile(t, filepath.Join(root, "docs", "a.txt"), "a\n")
	writePreviewFile(t, filepath.Join(root, "docs", "b.txt"), "b\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	web := &fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"}
	previewer.SetWebPreviewPublisher(web)

	req := MarkdownPreviewRequest{
		GatewayID:        "main",
		SurfaceSessionID: "feishu:main:user:ou_user",
		ActorUserID:      "ou_user",
		PreviewGrantKey:  "message|main|surface|thread|turn|item",
		Block: render.Block{
			ID:       "item-1",
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "item-1",
			Kind:     render.BlockAssistantMarkdown,
			Final:    true,
			Text:     "Open [a](docs/a.txt) and [b](docs/b.txt).",
		},
	}
	if _, err := previewer.RewriteFinalBlock(context.Background(), req); err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if len(web.issuedFor) != 2 {
		t.Fatalf("expected two publisher calls, got %#v", web.issuedFor)
	}
	if web.issuedFor[0].GrantKey != req.PreviewGrantKey || web.issuedFor[1].GrantKey != req.PreviewGrantKey {
		t.Fatalf("expected stable grant key %q, got %#v", req.PreviewGrantKey, web.issuedFor)
	}
	if web.issuedFor[0].ScopePublicID != web.issuedFor[1].ScopePublicID {
		t.Fatalf("expected same scope public id, got %#v", web.issuedFor)
	}
}

func TestDriveMarkdownPreviewerFallsBackToWebPreviewWhenDriveUploadFails(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "design.md"), "# design\n")
	api := newFakePreviewAPI()
	api.uploadFileFunc = func(_ context.Context, _, _ string, _ []byte) (string, error) {
		return "", fmt.Errorf("upload failed")
	}
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
		StatePath:  filepath.Join(root, "state", "preview.json"),
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	web := &fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"}
	previewer.SetWebPreviewPublisher(web)

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Read [design](docs/design.md).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if !strings.HasPrefix(result.Block.Text, "Read [design](https://preview.example/g/shared/") {
		t.Fatalf("expected web preview fallback link, got %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 {
		t.Fatalf("expected drive publish to be attempted once, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerWebPreviewTracksPreviousVersion(t *testing.T) {
	root := t.TempDir()
	docPath := writePreviewFile(t, filepath.Join(root, "docs", "note.txt"), "v1\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Read [note](docs/note.txt).",
		},
	}
	if _, err := previewer.RewriteFinalBlock(context.Background(), req); err != nil {
		t.Fatalf("first rewrite: %v", err)
	}
	writePreviewFile(t, docPath, "v2\n")
	if _, err := previewer.RewriteFinalBlock(context.Background(), req); err != nil {
		t.Fatalf("second rewrite: %v", err)
	}

	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	scopePublicID := previewScopePublicID(scopeKey)
	manifest, err := previewer.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest == nil || len(manifest.Records) != 2 {
		t.Fatalf("unexpected manifest record count: %#v", manifest)
	}
	canonicalDocPath, err := previewCanonicalPath(docPath)
	if err != nil {
		t.Fatalf("canonical doc path: %v", err)
	}
	first := findWebPreviewRecordBySourceAndHash(manifest, canonicalDocPath, sha256Hex("v1\n"))
	second := findWebPreviewRecordBySourceAndHash(manifest, canonicalDocPath, sha256Hex("v2\n"))
	if first == nil || second == nil {
		t.Fatalf("missing second record in manifest: %#v", manifest.Records)
	}
	if second.PreviousID != first.PreviewID {
		t.Fatalf("expected previous id %q, got %#v", first.PreviewID, second)
	}
}

func TestDriveMarkdownPreviewerServesWebPreviewSnapshot(t *testing.T) {
	root := t.TempDir()
	notePath := writePreviewFile(t, filepath.Join(root, "docs", "note.txt"), "hello preview\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Read [note](docs/note.txt).",
		},
	}
	if _, err := previewer.RewriteFinalBlock(context.Background(), req); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	scopePublicID := previewScopePublicID(scopeKey)
	manifest, err := previewer.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	canonicalNotePath, err := previewCanonicalPath(notePath)
	if err != nil {
		t.Fatalf("canonical note path: %v", err)
	}
	record := findWebPreviewRecordBySourceAndHash(manifest, canonicalNotePath, sha256Hex("hello preview\n"))
	if record == nil {
		t.Fatalf("missing preview record in manifest: %#v", manifest)
	}
	previewID := record.PreviewID
	httpReq := httptest.NewRequest(http.MethodGet, "/preview/s/"+scopePublicID+"/"+previewID, nil)
	rec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(rec, httpReq, scopePublicID, previewID, false); !ok {
		t.Fatal("expected preview route to be served")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello preview") {
		t.Fatalf("expected preview body to include content, got %q", rec.Body.String())
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

func TestSplitPreviewLocationSuffixSupportsGenericFileLocations(t *testing.T) {
	tests := []struct {
		target     string
		wantBase   string
		wantLine   int
		wantColumn int
		wantSuffix string
	}{
		{target: "docs/design.md:50", wantBase: "docs/design.md", wantLine: 50, wantSuffix: ":50"},
		{target: "internal/main.go:92:5", wantBase: "internal/main.go", wantLine: 92, wantColumn: 5, wantSuffix: ":92:5"},
		{target: "internal/main.go#L92C5", wantBase: "internal/main.go", wantLine: 92, wantColumn: 5, wantSuffix: "#L92C5"},
		{target: "internal/main.go:", wantBase: "internal/main.go:", wantLine: 0, wantColumn: 0, wantSuffix: ""},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			base, location, suffix := splitPreviewLocationSuffix(tt.target)
			if base != tt.wantBase || location.Line != tt.wantLine || location.Column != tt.wantColumn || suffix != tt.wantSuffix {
				t.Fatalf("splitPreviewLocationSuffix(%q) = (%q, %#v, %q), want (%q, line=%d col=%d, %q)",
					tt.target, base, location, suffix, tt.wantBase, tt.wantLine, tt.wantColumn, tt.wantSuffix)
			}
		})
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

func firstWebPreviewRecord(manifest *webPreviewScopeManifest) *webPreviewRecord {
	if manifest == nil {
		return nil
	}
	for _, record := range manifest.Records {
		if record != nil {
			return record
		}
	}
	return nil
}

func findWebPreviewRecordBySourceAndHash(manifest *webPreviewScopeManifest, sourcePath, contentHash string) *webPreviewRecord {
	if manifest == nil {
		return nil
	}
	for _, record := range manifest.Records {
		if record == nil {
			continue
		}
		if record.SourcePath == sourcePath && record.ContentHash == contentHash {
			return record
		}
	}
	return nil
}

func assertPreviewContextHasDeadlineWithin(t *testing.T, ctx context.Context, max time.Duration) {
	t.Helper()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("expected future deadline, got %s", deadline)
	}
	if remaining > max+time.Second {
		t.Fatalf("expected deadline within %s, got remaining %s", max, remaining)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
