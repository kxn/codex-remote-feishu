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
	block1, err := previewer1.RewriteFinalBlock(context.Background(), req)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if want := "See [design](https://preview/file-1)."; block1.Text != want {
		t.Fatalf("unexpected rewritten text: %q", block1.Text)
	}
	if len(api1.createFolderCalls) != 3 {
		t.Fatalf("expected root + marker + scope folder creation, got %#v", api1.createFolderCalls)
	}
	if len(api1.uploadFileCalls) != 1 {
		t.Fatalf("expected one upload, got %#v", api1.uploadFileCalls)
	}
	if api1.uploadFileCalls[0].ParentToken != "folder-3" {
		t.Fatalf("expected upload into scope folder, got %#v", api1.uploadFileCalls[0])
	}
	if !strings.HasPrefix(api1.uploadFileCalls[0].FileName, previewManagedFilePrefix+"design--") || !strings.HasSuffix(api1.uploadFileCalls[0].FileName, ".md") {
		t.Fatalf("unexpected uploaded file name: %#v", api1.uploadFileCalls[0])
	}
	if api1.uploadFileCalls[0].Content != "# design\n" {
		t.Fatalf("unexpected uploaded content: %#v", api1.uploadFileCalls[0])
	}
	if len(api1.grantPermissionCalls) != 2 {
		t.Fatalf("expected folder + file grants, got %#v", api1.grantPermissionCalls)
	}
	if api1.grantPermissionCalls[0].Token != "folder-3" || api1.grantPermissionCalls[1].Token != "file-1" {
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
		"folder-3|openid:ou_user":   true,
		"folder-3|openchat:oc_chat": true,
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
			Token:       "fld-root",
			URL:         "https://preview/fld-root",
			MarkerReady: true,
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

func TestDriveMarkdownPreviewerReconcileDetectsMissingRemoteNodesAndPermissionDrift(t *testing.T) {
	state := &previewState{
		Root: &previewFolderRecord{
			Token:       "fld-root",
			URL:         "https://preview/fld-root",
			MarkerReady: true,
		},
		Scopes: map[string]*previewScopeRecord{
			"feishu:app-1:chat:oc_main": {
				Folder: &previewFolderRecord{
					Token:  "fld-main",
					URL:    "https://preview/fld-main",
					Shared: map[string]bool{"openchat:oc_main": true},
				},
			},
			"feishu:app-1:chat:oc_missing": {
				Folder: &previewFolderRecord{
					Token: "fld-missing",
					URL:   "https://preview/fld-missing",
				},
			},
		},
		Files: map[string]*previewFileRecord{
			"feishu:app-1:chat:oc_main|/repo/docs/main.md|sha-main": {
				Path:      "/repo/docs/main.md",
				Token:     "file-main",
				URL:       "https://preview/file-main",
				ScopeKey:  "feishu:app-1:chat:oc_main",
				Shared:    map[string]bool{"openid:ou_user": true},
				CreatedAt: time.Now().UTC(),
			},
			"feishu:app-1:chat:oc_main|/repo/docs/missing.md|sha-missing": {
				Path:      "/repo/docs/missing.md",
				Token:     "file-missing",
				URL:       "https://preview/file-missing",
				ScopeKey:  "feishu:app-1:chat:oc_main",
				CreatedAt: time.Now().UTC(),
			},
		},
	}

	api := newFakePreviewAPI()
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		switch folderToken {
		case "fld-root":
			return []previewRemoteNode{
				{Token: "fld-main", Type: previewFolderType},
				{Token: "fld-orphan", Type: previewFolderType},
			}, nil
		case "fld-main":
			return []previewRemoteNode{
				{Token: "file-main", Type: previewFileType},
				{Token: "file-orphan", Type: previewFileType},
			}, nil
		default:
			return nil, &driveAPIError{Code: 1061003, Msg: "missing"}
		}
	}
	api.listPermissionFunc = func(_ context.Context, token, _ string) (map[string]bool, error) {
		switch token {
		case "fld-main":
			return map[string]bool{"openchat:oc_main": true}, nil
		case "file-main":
			return map[string]bool{}, nil
		default:
			return map[string]bool{}, nil
		}
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{StatePath: filepath.Join(t.TempDir(), "preview.json")})
	previewer.loaded = true
	previewer.state = normalizePreviewState(state)

	result, err := previewer.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if result.RemoteMissingScopeCount != 1 || result.RemoteMissingFileCount != 1 {
		t.Fatalf("unexpected remote missing counts: %#v", result)
	}
	if result.LocalOnlyScopeCount != 1 || result.LocalOnlyFileCount != 1 {
		t.Fatalf("unexpected local-only counts: %#v", result)
	}
	if result.PermissionDriftCount != 1 {
		t.Fatalf("expected one permission drift, got %#v", result)
	}
}

func TestDriveMarkdownPreviewerRecreatesMissingScopeFolder(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "plan.md"), "# plan\n")
	statePath := filepath.Join(root, "state", "preview.json")

	initialState := &previewState{
		Root: &previewFolderRecord{Token: "fld-root", URL: "https://preview/fld-root", MarkerReady: true},
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
				{Token: "fld-marker", Type: previewFolderType, Name: previewRootMarkerFolderName("main")},
				{Token: "fld-scope", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-scope":
			return []previewRemoteNode{
				{
					Token:       "file-old",
					Type:        previewFileType,
					Name:        previewManagedFilePrefix + "old--deadbeef.md",
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

func TestDriveMarkdownPreviewerLazyCleanupRunsAtMostOncePerInterval(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "one.md"), "# one\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "two.md"), "# two\n")

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
				{Token: "fld-marker", Type: previewFolderType, Name: previewRootMarkerFolderName("main")},
				{Token: "fld-old", Type: previewFolderType, Name: "feishu-main-chat-oc_old"},
			}, nil
		case "fld-old":
			return []previewRemoteNode{
				{
					Token:       "file-old",
					Type:        previewFileType,
					Name:        previewManagedFilePrefix + "legacy--deadbeef.md",
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
	block, err := previewer.RewriteFinalBlock(context.Background(), req1)
	if err != nil {
		t.Fatalf("first rewrite returned error: %v", err)
	}
	if block.Text != "Open [one](https://preview/file-1)." {
		t.Fatalf("unexpected first rewrite: %q", block.Text)
	}
	if len(api.deleteFileCalls) != 1 || api.deleteFileCalls[0].Token != "file-old" {
		t.Fatalf("expected first upload to cleanup one old managed file, got %#v", api.deleteFileCalls)
	}

	now = now.Add(1 * time.Hour)
	req2 := MarkdownPreviewRequest{
		GatewayID:        "main",
		SurfaceSessionID: "feishu:main:chat:oc_chat",
		ChatID:           "oc_chat",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [two](docs/two.md).",
		},
	}
	block, err = previewer.RewriteFinalBlock(context.Background(), req2)
	if err != nil {
		t.Fatalf("second rewrite returned error: %v", err)
	}
	if block.Text != "Open [two](https://preview/file-2)." {
		t.Fatalf("unexpected second rewrite: %q", block.Text)
	}
	if len(api.deleteFileCalls) != 1 {
		t.Fatalf("expected lazy cleanup throttle to suppress a second cleanup run, got %#v", api.deleteFileCalls)
	}
	if len(api.uploadFileCalls) != 2 {
		t.Fatalf("expected both rewrites to upload their own preview files, got %#v", api.uploadFileCalls)
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

func TestPreviewPathCandidatesTreatWindowsDrivePathAsAbsolute(t *testing.T) {
	roots := []string{`D:\Work\GoDot\interview-simulator`}
	target := `d:\Work\GoDot\interview-simulator\docs\characters.md`

	candidates := previewPathCandidates(target, roots)

	if len(candidates) != 1 || candidates[0] != target {
		t.Fatalf("expected absolute windows path to bypass root join, got %#v", candidates)
	}
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
