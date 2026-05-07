package install

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const defaultReleaseRepository = "kxn/codex-remote-feishu"

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ReleaseInfo struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets,omitempty"`
}

type ReleaseLookupOptions struct {
	Repository     string
	ReleasesAPIURL string
	Track          ReleaseTrack
	Client         *http.Client
}

var (
	productionReleasePattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	betaReleasePattern       = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$`)
	alphaReleasePattern      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+$`)
)

func ResolveLatestRelease(ctx context.Context, opts ReleaseLookupOptions) (ReleaseInfo, error) {
	track := normalizeReleaseTrack(opts.Track)
	if track == "" {
		return ReleaseInfo{}, fmt.Errorf("unsupported release track %q", opts.Track)
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{}
	}
	apiURL := strings.TrimSpace(opts.ReleasesAPIURL)
	if apiURL == "" {
		repo := strings.TrimSpace(opts.Repository)
		if repo == "" {
			repo = defaultReleaseRepository
		}
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100", repo)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("release lookup failed: http %d", resp.StatusCode)
	}

	var releases []ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return ReleaseInfo{}, err
	}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		if releaseMatchesTrack(release, track) {
			return release, nil
		}
	}
	return ReleaseInfo{}, fmt.Errorf("latest %s release not found", track)
}

func releaseMatchesTrack(release ReleaseInfo, track ReleaseTrack) bool {
	tag := strings.TrimSpace(release.TagName)
	switch track {
	case ReleaseTrackProduction:
		return !release.Prerelease && productionReleasePattern.MatchString(tag)
	case ReleaseTrackBeta:
		return release.Prerelease && betaReleasePattern.MatchString(tag)
	case ReleaseTrackAlpha:
		return release.Prerelease && alphaReleasePattern.MatchString(tag)
	default:
		return false
	}
}
