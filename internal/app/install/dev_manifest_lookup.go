package install

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const defaultDevManifestTag = "dev-latest"

type DevManifestAsset struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type DevManifest struct {
	Channel string             `json:"channel"`
	Version string             `json:"version"`
	Commit  string             `json:"commit"`
	BuiltAt string             `json:"built_at"`
	Assets  []DevManifestAsset `json:"assets,omitempty"`
}

type DevManifestLookupOptions struct {
	Repository  string
	ManifestURL string
	Client      *http.Client
}

func ResolveDevManifest(ctx context.Context, opts DevManifestLookupOptions) (DevManifest, DevManifestAsset, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	if manifestURL == "" {
		manifestURL = defaultDevManifestURL(strings.TrimSpace(opts.Repository))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return DevManifest{}, DevManifestAsset{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return DevManifest{}, DevManifestAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DevManifest{}, DevManifestAsset{}, fmt.Errorf("dev manifest lookup failed: http %d", resp.StatusCode)
	}

	var manifest DevManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return DevManifest{}, DevManifestAsset{}, err
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return DevManifest{}, DevManifestAsset{}, fmt.Errorf("dev manifest missing version")
	}
	asset, err := selectDevManifestAsset(manifest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return DevManifest{}, DevManifestAsset{}, err
	}
	return manifest, asset, nil
}

func defaultDevManifestURL(repository string) string {
	repo := strings.TrimSpace(repository)
	if repo == "" {
		repo = defaultReleaseRepository
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/dev-latest.json", repo, defaultDevManifestTag)
}

func selectDevManifestAsset(manifest DevManifest, goos, goarch string) (DevManifestAsset, error) {
	for _, asset := range manifest.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.GOOS), goos) && strings.EqualFold(strings.TrimSpace(asset.GOARCH), goarch) {
			if strings.TrimSpace(asset.Name) == "" {
				return DevManifestAsset{}, fmt.Errorf("dev manifest asset for %s/%s is missing name", goos, goarch)
			}
			if strings.TrimSpace(asset.URL) == "" {
				return DevManifestAsset{}, fmt.Errorf("dev manifest asset %s is missing url", asset.Name)
			}
			return asset, nil
		}
	}
	return DevManifestAsset{}, fmt.Errorf("dev manifest does not contain asset for %s/%s", goos, goarch)
}
