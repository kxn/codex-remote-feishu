package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriveMarkdownPreviewerServesHTMLAndSVGAsSourcePreview(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, htmlPreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "unsafe.html"), []byte("<script>alert(1)</script>\n"), time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC))
	htmlRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(htmlRec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, htmlPreviewID, false); !ok {
		t.Fatal("expected html preview to be served")
	}
	if htmlRec.Code != http.StatusOK {
		t.Fatalf("html preview status = %d, want 200", htmlRec.Code)
	}
	if !strings.Contains(htmlRec.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected html source preview to escape script body, got %q", htmlRec.Body.String())
	}
	if strings.Contains(htmlRec.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("expected html preview to stay non-executable, got %q", htmlRec.Body.String())
	}

	_, svgPreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), time.Date(2026, 4, 15, 10, 1, 0, 0, time.UTC))
	svgRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(svgRec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, svgPreviewID, false); !ok {
		t.Fatal("expected svg preview to be served")
	}
	if svgRec.Code != http.StatusOK {
		t.Fatalf("svg preview status = %d, want 200", svgRec.Code)
	}
	if !strings.Contains(svgRec.Body.String(), "不会作为同源文档直接渲染") {
		t.Fatalf("expected svg safety notice, got %q", svgRec.Body.String())
	}
	if strings.Contains(svgRec.Body.String(), `<img src="./download?inline=1"`) {
		t.Fatalf("expected svg preview to avoid inline image mode, got %q", svgRec.Body.String())
	}
}

func TestDriveMarkdownPreviewerServesImageAndPDFInsidePreviewShell(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, imagePreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "shot.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC))
	imageRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(imageRec, httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+imagePreviewID, nil), testPreviewScopePublicID, imagePreviewID, false); !ok {
		t.Fatal("expected image preview to be served")
	}
	if !strings.Contains(imageRec.Body.String(), `class="preview-topbar"`) {
		t.Fatalf("expected image preview to use shared shell, got %q", imageRec.Body.String())
	}
	if !strings.Contains(imageRec.Body.String(), `<img class="preview-image" src="`+imagePreviewID+`/download?inline=1"`) {
		t.Fatalf("expected image preview shell, got %q", imageRec.Body.String())
	}

	_, pdfPreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "design.pdf"), []byte("%PDF-1.4\n"), time.Date(2026, 4, 15, 11, 1, 0, 0, time.UTC))
	pdfRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(pdfRec, httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+pdfPreviewID, nil), testPreviewScopePublicID, pdfPreviewID, false); !ok {
		t.Fatal("expected pdf preview to be served")
	}
	if !strings.Contains(pdfRec.Body.String(), `<iframe class="preview-pdf" src="`+pdfPreviewID+`/download?inline=1"`) {
		t.Fatalf("expected pdf preview shell, got %q", pdfRec.Body.String())
	}
}

func TestDriveMarkdownPreviewerUsesRelativeDownloadLinks(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "note.txt"), []byte("hello\n"), time.Date(2026, 4, 17, 8, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/g/grant-1/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected text preview to be served")
	}
	if !strings.Contains(rec.Body.String(), `class="preview-download" href="`+previewID+`/download"`) {
		t.Fatalf("expected topbar download link to use preview-relative href, got %q", rec.Body.String())
	}
}

func TestDriveMarkdownPreviewerUsesSameRelativeDownloadLinksUnderInternalPath(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "note.txt"), []byte("hello\n"), time.Date(2026, 4, 17, 8, 5, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID, nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected proxied text preview to be served")
	}
	if !strings.Contains(rec.Body.String(), `class="preview-download" href="`+previewID+`/download"`) {
		t.Fatalf("expected topbar download link to stay preview-relative on internal path, got %q", rec.Body.String())
	}
}

func TestDriveMarkdownPreviewerServesDiffFirstForLargeText(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	originalThreshold := previewDiffFirstThresholdBytes
	previewDiffFirstThresholdBytes = 1
	defer func() { previewDiffFirstThresholdBytes = originalThreshold }()

	sourcePath := filepath.Join(root, "docs", "note.txt")
	publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("alpha\n"), time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC))
	_, secondPreviewID := publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("beta\n"), time.Date(2026, 4, 15, 12, 1, 0, 0, time.UTC))

	rec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(rec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, secondPreviewID, false); !ok {
		t.Fatal("expected diff-first preview to be served")
	}
	body := rec.Body.String()
	if strings.Contains(body, "类型：") {
		t.Fatalf("expected minimal shell without legacy type metadata, got %q", body)
	}
	if !strings.Contains(body, "preview-topbar") || !strings.Contains(body, "-alpha") || !strings.Contains(body, "+beta") {
		t.Fatalf("expected unified diff preview, got %q", body)
	}
}

func TestDriveMarkdownPreviewerServesLineAddressedSourcePreview(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "internal", "main.go"), []byte("package main\n\nfunc main() {}\n"), time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID+"?loc=L3C6", nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected line-addressed preview to be served")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="L3"`) || !strings.Contains(body, `class="source-line source-line--target"`) {
		t.Fatalf("expected target line anchor and highlight, got %q", body)
	}
	if !strings.Contains(body, `href="#L3"`) || !strings.Contains(body, "已定位到第 3 行 第 6 列") {
		t.Fatalf("expected line-addressed navigation notice, got %q", body)
	}
	if !strings.Contains(body, `class="source-column-target"`) {
		t.Fatalf("expected target column highlight, got %q", body)
	}
}

func TestDriveMarkdownPreviewerUsesSourceViewForMarkdownLocationPreview(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, previewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "design.md"), []byte("# Title\n\nBody\n"), time.Date(2026, 4, 17, 9, 5, 0, 0, time.UTC))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/preview/s/"+testPreviewScopePublicID+"/"+previewID+"?loc=L3", nil)
	if ok := previewer.ServeWebPreview(rec, req, testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected markdown location preview to be served")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "当前按源码视图展示") {
		t.Fatalf("expected markdown location preview to explain source fallback, got %q", body)
	}
	if strings.Contains(body, `class="preview-prose"`) {
		t.Fatalf("expected markdown location preview to avoid rendered article mode, got %q", body)
	}
	if !strings.Contains(body, `id="L3"`) {
		t.Fatalf("expected markdown location preview to expose source line anchors, got %q", body)
	}
}

func TestDriveMarkdownPreviewerReturnsExpiredAndMissingPreviewResponses(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	sourcePath := filepath.Join(root, "docs", "note.txt")
	_, previewID := publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("hello\n"), time.Date(2026, 4, 15, 13, 0, 0, 0, time.UTC))

	manifest, err := previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	manifest.Records[previewID].ExpiresAt = time.Date(2026, 4, 15, 13, 0, 30, 0, time.UTC)
	if err := previewer.saveWebPreviewScopeManifest(manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	previewer.nowFn = func() time.Time { return time.Date(2026, 4, 15, 13, 1, 0, 0, time.UTC) }

	expiredRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(expiredRec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected expired preview response")
	}
	if expiredRec.Code != http.StatusGone {
		t.Fatalf("expired preview status = %d, want 410 body=%s", expiredRec.Code, expiredRec.Body.String())
	}
	if !strings.Contains(expiredRec.Body.String(), "preview-topbar") {
		t.Fatalf("expected expired preview to use shared shell, got %q", expiredRec.Body.String())
	}

	missingRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(missingRec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, "../other", false); ok {
		t.Fatalf("expected missing/tampered preview id to be rejected, got status=%d body=%q", missingRec.Code, missingRec.Body.String())
	}
}

func TestDriveMarkdownPreviewerServesScopeRootInsideSharedShell(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	rec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(rec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, "", false); !ok {
		t.Fatal("expected scope root preview response")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("scope root preview status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="preview-topbar"`) || !strings.Contains(body, "请回到原消息并点击其中的具体文件链接进入预览") {
		t.Fatalf("expected scope root page inside shared shell, got %q", body)
	}
	if strings.Contains(body, `class="hero"`) || strings.Contains(body, `class="panel"`) {
		t.Fatalf("expected scope root page to avoid legacy admin shell, got %q", body)
	}
}

func TestDriveMarkdownPreviewerTreatsMissingBlobAsExpiredPreview(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	now := time.Date(2026, 4, 15, 13, 30, 0, 0, time.UTC)
	sourcePath := filepath.Join(root, "docs", "note.txt")
	_, previewID := publishWebPreviewArtifactForTest(t, previewer, sourcePath, []byte("hello\n"), now)

	manifest, err := previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	record := manifest.Records[previewID]
	if record == nil {
		t.Fatalf("missing preview record %q", previewID)
	}
	originalExpiresAt := record.ExpiresAt
	if err := os.Remove(previewer.previewBlobPath(record.BlobKey)); err != nil {
		t.Fatalf("remove preview blob: %v", err)
	}

	previewer.nowFn = func() time.Time { return now.Add(time.Minute) }
	rec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(rec, httptest.NewRequest(http.MethodGet, "/preview", nil), testPreviewScopePublicID, previewID, false); !ok {
		t.Fatal("expected missing blob preview to produce an expired response")
	}
	if rec.Code != http.StatusGone {
		t.Fatalf("missing blob preview status = %d, want 410 body=%s", rec.Code, rec.Body.String())
	}

	manifestAfter, err := previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if got := manifestAfter.Records[previewID].ExpiresAt; !got.Equal(originalExpiresAt) {
		t.Fatalf("expected missing blob preview not to refresh ttl, got %s want %s", got, originalExpiresAt)
	}
}

func TestDriveMarkdownPreviewerDownloadInlineOnlyForSafeRenderers(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	_, htmlPreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "unsafe.html"), []byte("<b>unsafe</b>"), time.Date(2026, 4, 15, 14, 0, 0, 0, time.UTC))
	htmlReq := httptest.NewRequest(http.MethodGet, "/preview/download?inline=1", nil)
	htmlRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(htmlRec, htmlReq, testPreviewScopePublicID, htmlPreviewID, true); !ok {
		t.Fatal("expected html download response")
	}
	if got := htmlRec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Fatalf("expected html inline request to stay attachment, got %q", got)
	}

	_, imagePreviewID := publishWebPreviewArtifactForTest(t, previewer, filepath.Join(root, "docs", "shot.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, time.Date(2026, 4, 15, 14, 1, 0, 0, time.UTC))
	imageReq := httptest.NewRequest(http.MethodGet, "/preview/download?inline=1", nil)
	imageRec := httptest.NewRecorder()
	if ok := previewer.ServeWebPreview(imageRec, imageReq, testPreviewScopePublicID, imagePreviewID, true); !ok {
		t.Fatal("expected image download response")
	}
	if got := imageRec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") {
		t.Fatalf("expected image inline download, got %q", got)
	}
}

const testPreviewScopeKey = "feishu:app-1:user:ou_user"

var testPreviewScopePublicID = previewScopePublicID(testPreviewScopeKey)

func newWebPreviewerForTest(root string) *DriveMarkdownPreviewer {
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})
	return previewer
}

func publishWebPreviewArtifactForTest(t *testing.T, previewer *DriveMarkdownPreviewer, sourcePath string, content []byte, now time.Time) (string, string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, content, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	previewer.nowFn = func() time.Time { return now }
	req := PreviewPublishRequest{
		ScopeKey: testPreviewScopeKey,
		Plan: PreviewPlan{
			Artifact: PreparedPreviewArtifact{
				SourcePath:   sourcePath,
				DisplayName:  filepath.Base(sourcePath),
				ContentHash:  sha256BytesHex(content),
				ArtifactKind: artifactKindForTest(sourcePath),
				MIMEType:     mimeTypeForTest(sourcePath),
				Bytes:        content,
			},
		},
	}
	if _, err := previewer.publishWebPreviewArtifact(context.Background(), req); err != nil {
		t.Fatalf("publish web preview artifact: %v", err)
	}
	return testPreviewScopePublicID, previewRecordID(testPreviewScopeKey, sourcePath, sha256BytesHex(content))
}

func artifactKindForTest(path string) string {
	kind, _, ok := previewArtifactMetadata(path)
	if !ok {
		return "binary"
	}
	return kind
}

func mimeTypeForTest(path string) string {
	_, mimeType, ok := previewArtifactMetadata(path)
	if !ok {
		return "application/octet-stream"
	}
	return mimeType
}

func sha256BytesHex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
