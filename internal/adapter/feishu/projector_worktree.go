package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
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
	info, err := gitmeta.InspectWorkspace(cwd, gitmeta.InspectOptions{IncludeStatus: true})
	if err != nil || !info.InRepo() {
		return nil
	}
	return worktreeSummaryFromInfo(info)
}

func parseGitStatusPaths(output string) []string {
	return gitmeta.ParseStatusPaths(output)
}

func parseGitWorktreeSummary(output string) *gitWorktreeSummary {
	status := gitmeta.ParseStatusSummary(output)
	return &gitWorktreeSummary{
		Dirty:          status.Dirty,
		Files:          status.Files,
		ModifiedCount:  status.ModifiedCount,
		UntrackedCount: status.UntrackedCount,
	}
}

func worktreeSummaryFromInfo(info gitmeta.WorkspaceInfo) *gitWorktreeSummary {
	if !info.InRepo() {
		return nil
	}
	return &gitWorktreeSummary{
		Branch:         strings.TrimSpace(info.Branch),
		Dirty:          info.Status.Dirty,
		Files:          info.Status.Files,
		ModifiedCount:  info.Status.ModifiedCount,
		UntrackedCount: info.Status.UntrackedCount,
	}
}
