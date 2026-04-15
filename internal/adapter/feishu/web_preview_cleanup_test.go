package feishu

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDriveMarkdownPreviewerCleanupWebPreviewCacheRemovesExpiredRecordsAndStaleBlobs(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	now := time.Date(2026, 4, 15, 15, 0, 0, 0, time.UTC)
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
	if err := os.Chtimes(previewer.previewBlobPath(expiredRecord.BlobKey), now.Add(-defaultPreviewBlobTTL-time.Hour), now.Add(-defaultPreviewBlobTTL-time.Hour)); err != nil {
		t.Fatalf("age expired blob: %v", err)
	}

	staleBlobKey := "deadbeefcafebabe"
	staleBlobPath := previewer.previewBlobPath(staleBlobKey)
	if err := os.MkdirAll(filepath.Dir(staleBlobPath), 0o755); err != nil {
		t.Fatalf("mkdir stale blob dir: %v", err)
	}
	if err := os.WriteFile(staleBlobPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale blob: %v", err)
	}
	if err := os.Chtimes(staleBlobPath, now.Add(-defaultPreviewBlobTTL-time.Hour), now.Add(-defaultPreviewBlobTTL-time.Hour)); err != nil {
		t.Fatalf("age stale blob: %v", err)
	}

	previewer.mu.Lock()
	err = previewer.cleanupWebPreviewCacheLocked(now)
	previewer.mu.Unlock()
	if err != nil {
		t.Fatalf("cleanup web preview cache: %v", err)
	}

	manifest, err = previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if manifest != nil && len(manifest.Records) != 0 {
		t.Fatalf("expected expired record to be pruned, got %#v", manifest.Records)
	}
	if _, err := os.Stat(previewer.previewBlobPath(expiredRecord.BlobKey)); !os.IsNotExist(err) {
		t.Fatalf("expected expired blob to be removed, got err=%v", err)
	}
	if _, err := os.Stat(staleBlobPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale unreferenced blob to be removed, got err=%v", err)
	}
}

func TestDriveMarkdownPreviewerCleanupWebPreviewCacheEvictsOldestRecordsOverBudget(t *testing.T) {
	root := t.TempDir()
	previewer := newWebPreviewerForTest(root)

	originalBudget := defaultPreviewCacheBudgetBytes
	defaultPreviewCacheBudgetBytes = 16
	defer func() { defaultPreviewCacheBudgetBytes = originalBudget }()

	now := time.Date(2026, 4, 15, 16, 0, 0, 0, time.UTC)
	sourcePathOld := filepath.Join(root, "docs", "old.txt")
	_, oldPreviewID := publishWebPreviewArtifactForTest(t, previewer, sourcePathOld, []byte("older payload"), now.Add(-time.Hour))
	sourcePathNew := filepath.Join(root, "docs", "new.txt")
	_, newPreviewID := publishWebPreviewArtifactForTest(t, previewer, sourcePathNew, []byte("newer payload"), now)

	previewer.mu.Lock()
	err := previewer.cleanupWebPreviewCacheLocked(now.Add(time.Minute))
	previewer.mu.Unlock()
	if err != nil {
		t.Fatalf("cleanup web preview cache: %v", err)
	}

	manifest, err := previewer.loadWebPreviewScopeManifest(testPreviewScopePublicID)
	if err != nil {
		t.Fatalf("reload manifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest to remain after budget eviction")
	}
	if manifest.Records[oldPreviewID] != nil {
		t.Fatalf("expected oldest preview record to be evicted, got %#v", manifest.Records)
	}
	if manifest.Records[newPreviewID] == nil {
		t.Fatalf("expected newest preview record to remain, got %#v", manifest.Records)
	}
	if _, err := os.Stat(previewer.previewBlobPath(manifest.Records[newPreviewID].BlobKey)); err != nil {
		t.Fatalf("expected newest blob to remain: %v", err)
	}
}
