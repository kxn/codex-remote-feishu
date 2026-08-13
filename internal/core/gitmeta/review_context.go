package gitmeta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultReviewContextLimit = 256 * 1024

type ReviewContext struct {
	Text      string
	Truncated bool
}

func BuildUncommittedReviewContext(path string, maxBytes int) (ReviewContext, error) {
	commandDir, err := reviewRepoRoot(path)
	if err != nil || commandDir == "" {
		return ReviewContext{}, err
	}
	staged, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "diff", "--cached", "--no-ext-diff", "--binary", "--")
	if err != nil {
		return ReviewContext{}, err
	}
	unstaged, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "diff", "--no-ext-diff", "--binary", "--")
	if err != nil {
		return ReviewContext{}, err
	}
	untrackedNames, err := runGitCommandRawOutput(commandDir, defaultCommitInspectTimeout, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return ReviewContext{}, err
	}

	var body strings.Builder
	status, err := runGitCommandRawOutput(commandDir, defaultCommitInspectTimeout, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return ReviewContext{}, err
	}
	writeReviewFileManifest(&body, ParseStatusPaths(status))
	writeReviewContextSection(&body, "Staged changes", staged)
	writeReviewContextSection(&body, "Unstaged changes", unstaged)
	if strings.TrimSpace(untrackedNames) != "" {
		body.WriteString("## Untracked files\n")
		for _, relativePath := range strings.Split(untrackedNames, "\x00") {
			if relativePath == "" {
				continue
			}
			body.WriteString("### ")
			body.WriteString(relativePath)
			body.WriteByte('\n')
			content, readErr := os.ReadFile(filepath.Join(commandDir, filepath.FromSlash(relativePath)))
			if readErr != nil {
				body.WriteString("[unable to read untracked file: ")
				body.WriteString(readErr.Error())
				body.WriteString("]\n")
				continue
			}
			body.Write(content)
			if len(content) == 0 || content[len(content)-1] != '\n' {
				body.WriteByte('\n')
			}
		}
	}
	return limitReviewContext(body.String(), maxBytes), nil
}

func BuildCommitReviewContext(path, commitSHA string, maxBytes int) (ReviewContext, error) {
	commandDir, err := reviewRepoRoot(path)
	if err != nil || commandDir == "" {
		return ReviewContext{}, err
	}
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return ReviewContext{}, fmt.Errorf("commit SHA is required")
	}
	files, err := runGitCommandRawOutput(commandDir, defaultCommitInspectTimeout, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", commitSHA, "--")
	if err != nil {
		return ReviewContext{}, err
	}
	output, err := runGitCommandOutput(commandDir, defaultCommitInspectTimeout, "show", "--no-ext-diff", "--no-show-signature", "--format=commit %H%nsubject: %s%n", "--binary", commitSHA, "--")
	if err != nil {
		return ReviewContext{}, err
	}
	var body strings.Builder
	writeReviewFileManifest(&body, strings.Split(files, "\x00"))
	body.WriteString("## Commit patch\n")
	body.WriteString(output)
	body.WriteByte('\n')
	return limitReviewContext(body.String(), maxBytes), nil
}

func reviewRepoRoot(path string) (string, error) {
	info, err := LocateWorkspace(path)
	if err != nil || !info.InRepo() {
		return "", err
	}
	return strings.TrimSpace(info.RepoRoot), nil
}

func writeReviewFileManifest(body *strings.Builder, files []string) {
	if len(files) == 0 {
		return
	}
	body.WriteString("## Changed files\n")
	for _, file := range files {
		file = strings.TrimSpace(file)
		if file != "" {
			body.WriteString("- ")
			body.WriteString(file)
			body.WriteByte('\n')
		}
	}
}

func writeReviewContextSection(body *strings.Builder, title, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	body.WriteString("## ")
	body.WriteString(title)
	body.WriteByte('\n')
	body.WriteString(content)
	body.WriteByte('\n')
}

func limitReviewContext(body string, maxBytes int) ReviewContext {
	if maxBytes <= 0 {
		maxBytes = defaultReviewContextLimit
	}
	if len(body) <= maxBytes {
		return ReviewContext{Text: body}
	}
	marker := fmt.Sprintf("\n[review context truncated at %d bytes; the changed-file manifest is complete, use Read, Glob, and Grep to inspect omitted content]\n", maxBytes)
	keep := maxBytes - len(marker)
	if keep < 0 {
		keep = 0
	}
	return ReviewContext{Text: body[:keep] + marker, Truncated: true}
}
