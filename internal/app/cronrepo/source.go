package cronrepo

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	InternalRootDirName = "cron-repos"
	RunsDirName         = "runs"
	CacheDirName        = "cache"
)

type SourceSpec struct {
	RawInput string
	RepoURL  string
	Ref      string
}

func (s SourceSpec) SourceKey() string {
	hasher := sha1.New()
	_, _ = hasher.Write([]byte(strings.TrimSpace(s.RepoURL)))
	_, _ = hasher.Write([]byte{'\n'})
	_, _ = hasher.Write([]byte(strings.TrimSpace(s.Ref)))
	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 24 {
		sum = sum[:24]
	}
	return sum
}

func (s SourceSpec) SourceLabel() string {
	repoLabel := repoDisplayLabel(strings.TrimSpace(s.RepoURL))
	if repoLabel == "" {
		repoLabel = strings.TrimSpace(s.RepoURL)
	}
	if repoLabel == "" {
		repoLabel = "unknown"
	}
	ref := strings.TrimSpace(s.Ref)
	if ref == "" {
		ref = "default"
	}
	return fmt.Sprintf("repo: %s @ %s", repoLabel, ref)
}

func ParseSourceInput(raw string) (SourceSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SourceSpec{}, &Error{Code: ErrorInvalidURL, Message: "git repo source input is required"}
	}
	base, ref := splitRefFragment(raw)
	spec, ok := parseTreeURL(base)
	if ok {
		if ref == "" {
			ref = spec.Ref
		}
		spec.RawInput = raw
		spec.Ref = strings.TrimSpace(ref)
		if strings.TrimSpace(spec.RepoURL) == "" {
			return SourceSpec{}, &Error{Code: ErrorInvalidURL, Message: "git repository url is invalid", SourceInput: raw}
		}
		return spec, nil
	}
	if !looksLikeGitRepoURL(base) {
		return SourceSpec{}, &Error{Code: ErrorInvalidURL, Message: "git repository url is invalid", SourceInput: raw}
	}
	return SourceSpec{
		RawInput: raw,
		RepoURL:  strings.TrimSpace(base),
		Ref:      strings.TrimSpace(ref),
	}, nil
}

func SQLiteRunPathLikePattern() string {
	return "%/" + InternalRootDirName + "/" + RunsDirName + "/%"
}

func splitRefFragment(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	marker := "#ref="
	index := strings.LastIndex(raw, marker)
	if index < 0 {
		return raw, ""
	}
	return strings.TrimSpace(raw[:index]), strings.TrimSpace(raw[index+len(marker):])
}

func parseTreeURL(raw string) (SourceSpec, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return SourceSpec{}, false
	}
	cleanPath := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if cleanPath == "" {
		return SourceSpec{}, false
	}
	parts := strings.Split(cleanPath, "/")
	switch {
	case len(parts) >= 4 && parts[2] == "tree":
		repoPath := path.Join(parts[0], parts[1]) + ".git"
		return SourceSpec{
			RepoURL: parsed.Scheme + "://" + parsed.Host + "/" + repoPath,
			Ref:     strings.Join(parts[3:], "/"),
		}, true
	case len(parts) >= 5 && parts[2] == "-" && parts[3] == "tree":
		repoPath := path.Join(parts[0], parts[1]) + ".git"
		return SourceSpec{
			RepoURL: parsed.Scheme + "://" + parsed.Host + "/" + repoPath,
			Ref:     strings.Join(parts[4:], "/"),
		}, true
	default:
		return SourceSpec{}, false
	}
}

func looksLikeGitRepoURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	switch {
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"), strings.HasPrefix(raw, "file://"):
		return true
	case strings.Contains(raw, "@") && strings.Contains(raw, ":"):
		return true
	default:
		return false
	}
}

func repoDisplayLabel(repoURL string) string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return ""
	}
	if parsed, err := url.Parse(repoURL); err == nil && parsed != nil && parsed.Host != "" {
		repoPath := strings.Trim(parsed.Path, "/")
		repoPath = strings.TrimSuffix(repoPath, ".git")
		if repoPath != "" {
			return parsed.Host + "/" + repoPath
		}
		return parsed.Host
	}
	if at := strings.Index(repoURL, "@"); at >= 0 {
		repoURL = repoURL[at+1:]
	}
	repoURL = strings.ReplaceAll(repoURL, ":", "/")
	repoURL = strings.Trim(repoURL, "/")
	repoURL = strings.TrimSuffix(repoURL, ".git")
	return repoURL
}
