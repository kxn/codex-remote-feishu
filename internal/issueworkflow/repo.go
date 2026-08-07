package issueworkflow

import "github.com/kxn/codex-remote-feishu/internal/ghclient"

// ParseRepo delegates to ghclient.ParseRepo.
func ParseRepo(value string) (Repo, error) {
	return ghclient.ParseRepo(value)
}

// RepoFromRemoteURL delegates to ghclient.RepoFromRemoteURL.
func RepoFromRemoteURL(remoteURL string) (Repo, error) {
	return ghclient.RepoFromRemoteURL(remoteURL)
}
