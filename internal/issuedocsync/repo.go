package issuedocsync

import "github.com/kxn/codex-remote-feishu/internal/ghclient"

// ParseRepo delegates to ghclient.ParseRepo.
func ParseRepo(value string) (Repo, error) {
	return ghclient.ParseRepo(value)
}
