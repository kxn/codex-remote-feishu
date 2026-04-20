package issueworkflow

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

type GitClient interface {
	TrackedDirtyFiles(context.Context) ([]string, error)
	PullFFOnly(context.Context) error
	CurrentBranch(context.Context) (string, error)
	HeadCommit(context.Context) (string, error)
	OriginRemoteURL(context.Context) (string, error)
	ChangedFilesFromHEAD(context.Context) ([]string, error)
	ChangedDocsNameStatus(context.Context) ([]string, error)
	DiffCheck(context.Context, bool) (string, error)
	GofmtList(context.Context, []string) ([]string, error)
}

type gitCLI struct {
	rootDir string
}

func NewGitCLI(rootDir string) GitClient {
	return &gitCLI{rootDir: rootDir}
}

func (g *gitCLI) TrackedDirtyFiles(ctx context.Context) ([]string, error) {
	output, err := g.run(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return nil, err
	}
	lines := splitNonEmptyLines(output)
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		files = append(files, filepath.Clean(strings.TrimSpace(line[3:])))
	}
	return files, nil
}

func (g *gitCLI) PullFFOnly(ctx context.Context) error {
	_, err := g.run(ctx, "pull", "--ff-only")
	return err
}

func (g *gitCLI) CurrentBranch(ctx context.Context) (string, error) {
	output, err := g.run(ctx, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (g *gitCLI) HeadCommit(ctx context.Context) (string, error) {
	output, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (g *gitCLI) OriginRemoteURL(ctx context.Context) (string, error) {
	output, err := g.run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func (g *gitCLI) ChangedFilesFromHEAD(ctx context.Context) ([]string, error) {
	output, err := g.run(ctx, "diff", "--name-only", "--diff-filter=ACMR", "HEAD")
	if err != nil {
		return nil, err
	}
	lines := splitNonEmptyLines(output)
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		files = append(files, filepath.Clean(line))
	}
	return files, nil
}

func (g *gitCLI) ChangedDocsNameStatus(ctx context.Context) ([]string, error) {
	output, err := g.run(ctx, "diff", "--name-status", "--find-renames", "HEAD", "--", "docs")
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(output), nil
}

func (g *gitCLI) DiffCheck(ctx context.Context, cached bool) (string, error) {
	args := []string{"diff", "--check"}
	if cached {
		args = append(args, "--cached")
	}
	return g.run(ctx, args...)
}

func (g *gitCLI) GofmtList(ctx context.Context, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	args := append([]string{"-l"}, files...)
	cmd := execlaunch.CommandContext(ctx, "gofmt", args...)
	cmd.Dir = g.rootDir
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(string(output)), nil
}

func (g *gitCLI) run(ctx context.Context, args ...string) (string, error) {
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = g.rootDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return strings.TrimSpace(string(output)), fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return strings.TrimSpace(string(output)), nil
}

func splitNonEmptyLines(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
