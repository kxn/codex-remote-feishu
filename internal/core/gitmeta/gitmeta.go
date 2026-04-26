package gitmeta

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ParseGitDirFile parses a gitdir pointer file (e.g. `.git` file in worktree)
// and returns the raw target path from `gitdir: ...`. It returns empty string
// when the file does not follow the gitdir pointer format.
func ParseGitDirFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", nil
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "gitdir:")), nil
}

// ResolveGitDirPath resolves a gitdir path against baseDir when the value is
// relative and normalizes the final path.
func ResolveGitDirPath(baseDir, gitDirPath string) string {
	baseDir = strings.TrimSpace(baseDir)
	gitDirPath = strings.TrimSpace(gitDirPath)
	if gitDirPath == "" {
		return ""
	}
	if !filepath.IsAbs(gitDirPath) && baseDir != "" {
		gitDirPath = filepath.Join(baseDir, gitDirPath)
	}
	return filepath.Clean(gitDirPath)
}

// FileHasExactTrimmedLine checks whether a file contains the exact line after
// trimming both file line and expected text.
func FileHasExactTrimmedLine(path, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == expected {
			return true
		}
	}
	return false
}
