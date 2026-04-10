package issueworkflow

import (
	"fmt"
	"strings"
)

func ParseRepo(value string) (Repo, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Repo{}, fmt.Errorf("missing repo value")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return Repo{}, fmt.Errorf("invalid repo %q, want owner/name", value)
	}
	return Repo{
		Owner: strings.TrimSpace(parts[0]),
		Name:  strings.TrimSpace(parts[1]),
	}, nil
}

func RepoFromRemoteURL(remoteURL string) (Repo, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	switch {
	case strings.HasPrefix(remoteURL, "https://github.com/"):
		return ParseRepo(strings.TrimPrefix(remoteURL, "https://github.com/"))
	case strings.HasPrefix(remoteURL, "git@github.com:"):
		return ParseRepo(strings.TrimPrefix(remoteURL, "git@github.com:"))
	default:
		return Repo{}, fmt.Errorf("unsupported origin remote %q", remoteURL)
	}
}
