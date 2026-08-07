package ghclient

import (
	"fmt"
	"strings"
)

// Repo identifies a GitHub repository by owner and name.
type Repo struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

// String returns "owner/name".
func (r Repo) String() string {
	if r.Owner == "" || r.Name == "" {
		return ""
	}
	return r.Owner + "/" + r.Name
}

// ParseRepo parses an "owner/name" string into a Repo.
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

// RepoFromRemoteURL parses a GitHub remote URL into a Repo.
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
