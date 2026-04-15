package install

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestResolveDevManifestSelectsCurrentPlatformAsset(t *testing.T) {
	firstGOOS := "linux"
	firstGOARCH := "amd64"
	if runtime.GOOS == firstGOOS && runtime.GOARCH == firstGOARCH {
		firstGOOS = "windows"
	}
	body := fmt.Sprintf(`{
  "channel": "dev",
  "version": "dev-abc123",
  "commit": "abc123def456",
  "built_at": "2026-04-15T01:02:03Z",
  "assets": [
    {
      "goos": %q,
      "goarch": %q,
      "name": "codex-remote-feishu_dev_linux_amd64.tar.gz",
      "url": "https://example.invalid/linux-amd64.tar.gz",
      "sha256": "deadbeef"
    },
    {
      "goos": %q,
      "goarch": %q,
      "name": "target",
      "url": "https://example.invalid/current",
      "sha256": "cafebabe"
    }
  ]
}`, firstGOOS, firstGOARCH, runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	manifest, asset, err := ResolveDevManifest(context.Background(), DevManifestLookupOptions{ManifestURL: server.URL})
	if err != nil {
		t.Fatalf("ResolveDevManifest: %v", err)
	}
	if manifest.Version != "dev-abc123" {
		t.Fatalf("manifest version = %q, want dev-abc123", manifest.Version)
	}
	if asset.Name != "target" {
		t.Fatalf("selected asset name = %q, want target", asset.Name)
	}
	if asset.URL != "https://example.invalid/current" {
		t.Fatalf("selected asset url = %q", asset.URL)
	}
}

func TestResolveDevManifestErrorsWhenPlatformAssetMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
  "channel": "dev",
  "version": "dev-abc123",
  "assets": [
    {
      "goos": "linux",
      "goarch": "arm64",
      "name": "other",
      "url": "https://example.invalid/other",
      "sha256": "cafebabe"
    }
  ]
}`))
	}))
	defer server.Close()

	_, _, err := ResolveDevManifest(context.Background(), DevManifestLookupOptions{ManifestURL: server.URL})
	if err == nil || !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("ResolveDevManifest error = %v, want missing platform asset", err)
	}
}
