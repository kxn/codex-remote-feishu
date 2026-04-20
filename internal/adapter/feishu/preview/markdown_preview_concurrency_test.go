package preview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestDriveMarkdownPreviewerConcurrentSameFileRewriteDedupesUpload(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "design.md"), "# design\n")

	api := newFakePreviewAPI()
	uploadStarted := make(chan struct{}, 1)
	uploadRelease := make(chan struct{})
	api.uploadFileFunc = func(_ context.Context, parentToken, fileName string, content []byte) (string, error) {
		select {
		case uploadStarted <- struct{}{}:
		default:
		}
		<-uploadRelease
		return "file-1", nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "state", "preview.json"),
		ProcessCWD: root,
	})

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "See [design](docs/design.md).",
		},
	}

	type rewriteOutcome struct {
		text string
		err  error
	}
	outcomes := make(chan rewriteOutcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := previewer.RewriteFinalBlock(context.Background(), req)
			outcomes <- rewriteOutcome{text: result.Block.Text, err: err}
		}()
	}

	select {
	case <-uploadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upload to start")
	}
	time.Sleep(50 * time.Millisecond)
	close(uploadRelease)
	wg.Wait()
	close(outcomes)

	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("rewrite returned error: %v", outcome.err)
		}
		if outcome.text != "See [design](https://preview/file-1)." {
			t.Fatalf("unexpected rewritten text: %q", outcome.text)
		}
	}

	api.mu.Lock()
	uploadCalls := len(api.uploadFileCalls)
	createCalls := len(api.createFolderCalls)
	api.mu.Unlock()
	if uploadCalls != 1 {
		t.Fatalf("expected one upload across concurrent rewrites, got %d", uploadCalls)
	}
	if createCalls != 2 {
		t.Fatalf("expected one root folder + one scope folder creation, got %d", createCalls)
	}
}

func TestDriveMarkdownPreviewerServeWebPreviewDoesNotBlockOnDriveRewrite(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "design.md"), "# design\n")

	api := newFakePreviewAPI()
	uploadStarted := make(chan struct{}, 1)
	uploadRelease := make(chan struct{})
	api.uploadFileFunc = func(_ context.Context, parentToken, fileName string, content []byte) (string, error) {
		select {
		case uploadStarted <- struct{}{}:
		default:
		}
		<-uploadRelease
		return "file-1", nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		StatePath:  filepath.Join(root, "state", "preview.json"),
		CacheDir:   filepath.Join(root, "preview-cache"),
		ProcessCWD: root,
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})

	sourcePath := filepath.Join(root, "docs", "note.txt")
	_, previewID := publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("hello preview\n"), time.Date(2026, 4, 15, 17, 0, 0, 0, time.UTC))

	req := MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Read [design](docs/design.md).",
		},
	}

	rewriteDone := make(chan error, 1)
	go func() {
		_, err := previewer.RewriteFinalBlock(context.Background(), req)
		rewriteDone <- err
	}()

	select {
	case <-uploadStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rewrite upload to start")
	}

	serveDone := make(chan struct {
		ok   bool
		code int
		body string
	}, 1)
	go func() {
		rec := httptest.NewRecorder()
		ok := previewer.ServeWebPreview(rec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, previewID, false)
		serveDone <- struct {
			ok   bool
			code int
			body string
		}{
			ok:   ok,
			code: rec.Code,
			body: rec.Body.String(),
		}
	}()

	select {
	case served := <-serveDone:
		if !served.ok {
			t.Fatal("expected preview route to be served while rewrite upload is blocked")
		}
		if served.code != http.StatusOK {
			t.Fatalf("expected 200 preview response, got %d body=%s", served.code, served.body)
		}
		if !strings.Contains(served.body, "hello preview") {
			t.Fatalf("expected served preview body to include content, got %q", served.body)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("web preview serve blocked behind drive rewrite")
	}

	close(uploadRelease)
	select {
	case err := <-rewriteDone:
		if err != nil {
			t.Fatalf("rewrite returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rewrite to finish")
	}
}

func TestDriveMarkdownPreviewerBackgroundCleanupDoesNotBlockRewrite(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "fresh.md"), "# fresh\n")

	api := newFakePreviewAPI()
	listStarted := make(chan struct{}, 1)
	listRelease := make(chan struct{})
	api.listFilesFunc = func(_ context.Context, folderToken string) ([]previewRemoteNode, error) {
		if folderToken == "fld-root" {
			select {
			case listStarted <- struct{}{}:
			default:
			}
			<-listRelease
		}
		return nil, nil
	}

	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		GatewayID:               "main",
		StatePath:               filepath.Join(root, "state", "preview.json"),
		ProcessCWD:              root,
		BackgroundCleanupEvery:  time.Minute,
		BackgroundCleanupMaxAge: 24 * time.Hour,
	})
	previewer.loaded = true
	previewer.state = normalizePreviewState(&previewState{
		Root: &previewFolderRecord{
			Token: "fld-root",
			URL:   "https://preview/fld-root",
		},
		Files: map[string]*previewFileRecord{
			"feishu:main:user:ou_user|/repo/docs/old.md|sha-old": {
				Path:       "/repo/docs/old.md",
				SHA256:     "sha-old",
				Token:      "file-old",
				URL:        "https://preview/file-old",
				ScopeKey:   "feishu:main:user:ou_user",
				SizeBytes:  10,
				CreatedAt:  time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
				LastUsedAt: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
			},
		},
	})
	previewer.nowFn = func() time.Time { return time.Date(2026, 4, 15, 18, 0, 0, 0, time.UTC) }

	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- previewer.runBackgroundCleanup(context.Background())
	}()

	select {
	case <-listStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background cleanup to enter remote listing")
	}

	rewriteDone := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
			GatewayID:        "main",
			SurfaceSessionID: "feishu:main:user:ou_user",
			ActorUserID:      "ou_user",
			WorkspaceRoot:    root,
			ThreadCWD:        root,
			Block: render.Block{
				Kind:  render.BlockAssistantMarkdown,
				Final: true,
				Text:  "Open [fresh](docs/fresh.md).",
			},
		})
		rewriteDone <- struct {
			text string
			err  error
		}{
			text: result.Block.Text,
			err:  err,
		}
	}()

	select {
	case outcome := <-rewriteDone:
		if outcome.err != nil {
			t.Fatalf("rewrite returned error: %v", outcome.err)
		}
		if outcome.text != "Open [fresh](https://preview/file-1)." {
			t.Fatalf("unexpected rewrite text: %q", outcome.text)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rewrite blocked behind background cleanup remote listing")
	}

	close(listRelease)
	select {
	case err := <-cleanupDone:
		if err != nil {
			t.Fatalf("background cleanup returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background cleanup to finish")
	}
}
