package feishu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

const maxEmbeddedWorktreePaths = 3

type gitWorktreeSummary struct {
	Branch         string
	Dirty          bool
	Files          []string
	ModifiedCount  int
	UntrackedCount int
}

func (p *Projector) formatFinalWorktreeSummaryLine(summary *control.FinalTurnSummary) string {
	if summary == nil || summary.Elapsed <= 0 {
		return ""
	}
	cwd := strings.TrimSpace(summary.ThreadCWD)
	if cwd == "" || p == nil || p.readGitWorktree == nil {
		return ""
	}
	worktree := p.readGitWorktree(cwd)
	if worktree == nil {
		return ""
	}
	if !worktree.Dirty {
		return "**工作区** " + formatNeutralTextTag("干净")
	}
	labels := shortestUniquePathSuffixes(worktree.Files)
	limit := len(worktree.Files)
	if limit > maxEmbeddedWorktreePaths {
		limit = maxEmbeddedWorktreePaths
	}
	parts := []string{"**工作区**", formatNeutralTextTag("有改动")}
	if worktree.ModifiedCount > 0 {
		parts = append(parts, formatNeutralTextTag(fmt.Sprintf("%d修改", worktree.ModifiedCount)))
	}
	if worktree.UntrackedCount > 0 {
		parts = append(parts, formatNeutralTextTag(fmt.Sprintf("%d未跟踪", worktree.UntrackedCount)))
	}
	for index := 0; index < limit; index++ {
		parts = append(parts, formatNeutralTextTag(fileChangeDisplayLabel(worktree.Files[index], labels)))
	}
	return strings.Join(parts, " ")
}

func inspectGitWorktreeSummary(cwd string) *gitWorktreeSummary {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return nil
	}
	output, ok := runGitInspector(cwd, "status", "--porcelain", "--untracked-files=all")
	if !ok {
		return nil
	}
	summary := parseGitWorktreeSummary(output)
	summary.Branch = inspectGitBranch(cwd)
	return summary
}

func runGitInspector(cwd string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func inspectGitBranch(cwd string) string {
	if output, ok := runGitInspector(cwd, "symbolic-ref", "--short", "HEAD"); ok {
		if branch := strings.TrimSpace(output); branch != "" {
			return branch
		}
	}
	if output, ok := runGitInspector(cwd, "rev-parse", "--short", "HEAD"); ok {
		if branch := strings.TrimSpace(output); branch != "" {
			return branch
		}
	}
	return ""
}

func parseGitStatusPaths(output string) []string {
	return parseGitWorktreeSummary(output).Files
}

func parseGitWorktreeSummary(output string) *gitWorktreeSummary {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	files := make([]string, 0, len(lines))
	modifiedSeen := map[string]bool{}
	untrackedSeen := map[string]bool{}
	modifiedCount := 0
	untrackedCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := strings.TrimSpace(line[3:])
		if idx := strings.LastIndex(path, " -> "); idx >= 0 {
			path = strings.TrimSpace(path[idx+4:])
		}
		path = normalizeFileSummaryPath(parseGitStatusPath(path))
		if path == "" {
			continue
		}
		if status == "??" {
			if !untrackedSeen[path] {
				untrackedSeen[path] = true
				untrackedCount++
			}
		} else if !modifiedSeen[path] {
			modifiedSeen[path] = true
			modifiedCount++
		}
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	return &gitWorktreeSummary{
		Dirty:          len(files) > 0,
		Files:          files,
		ModifiedCount:  modifiedCount,
		UntrackedCount: untrackedCount,
	}
}

func parseGitStatusPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && strings.HasPrefix(path, "\"") && strings.HasSuffix(path, "\"") {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
}
