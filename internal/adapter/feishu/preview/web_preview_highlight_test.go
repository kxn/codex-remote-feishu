package preview

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriveMarkdownPreviewerHighlightsSupportedSourcePreview(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "internal", "main.go"), []byte("package main\n\nfunc main() {}\n"), time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected go preview to be served")
	}

	body := rec.Body.String()
	if !strings.Contains(body, `class="preview-syntax preview-syntax--source"`) {
		t.Fatalf("expected highlighted source wrapper, got %q", body)
	}
	if !strings.Contains(body, `class="pv-chroma"`) || !strings.Contains(body, "pv-") {
		t.Fatalf("expected chroma-highlighted source preview, got %q", body)
	}
}

func TestDriveMarkdownPreviewerKeepsPlainTextPreviewUnhighlighted(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "note.txt"), []byte("alpha\nbeta\n"), time.Date(2026, 4, 17, 12, 5, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected text preview to be served")
	}

	body := rec.Body.String()
	if !strings.Contains(body, `<pre class="source-block">alpha`) {
		t.Fatalf("expected plain source preview, got %q", body)
	}
	if strings.Contains(body, `class="preview-syntax preview-syntax--source"`) || strings.Contains(body, `class="pv-chroma"`) {
		t.Fatalf("expected txt preview to stay unhighlighted, got %q", body)
	}
}

func TestDriveMarkdownPreviewerHighlightsMarkdownFencedCodeBlocks(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	content := []byte("# Title\n\n```go\npackage main\n```\n")
	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "design.md"), content, time.Date(2026, 4, 17, 12, 10, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected markdown preview to be served")
	}

	body := rec.Body.String()
	if !strings.Contains(body, `class="preview-prose"`) {
		t.Fatalf("expected rendered markdown article, got %q", body)
	}
	if !strings.Contains(body, `class="pv-chroma"`) || !strings.Contains(body, "pv-") {
		t.Fatalf("expected fenced go block to be highlighted, got %q", body)
	}
}

func TestDriveMarkdownPreviewerLeavesMarkdownCodeBlocksWithoutInfoStringPlain(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	content := []byte("# Title\n\n```\nplain\n```\n")
	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "design.md"), content, time.Date(2026, 4, 17, 12, 15, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected markdown preview to be served")
	}

	body := rec.Body.String()
	if strings.Contains(body, `class="pv-chroma"`) {
		t.Fatalf("expected code block without info string to stay plain, got %q", body)
	}
	if !strings.Contains(body, `<pre><code>plain`) {
		t.Fatalf("expected plain markdown code block fallback, got %q", body)
	}
}
