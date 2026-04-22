package cronruntime

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
)

type JobSourceType string

const (
	JobSourceWorkspace JobSourceType = "workspace"
	JobSourceGitRepo   JobSourceType = "git_repo"

	TaskSourceTypeField     = "来源类型"
	TaskWorkspaceField      = "工作区"
	TaskGitRepoInputField   = "Git 仓库引用"
	TaskConcurrencyField    = "并发度"
	TaskSourceWorkspaceText = "工作区"
	TaskSourceGitRepoText   = "Git 仓库"
)

func NormalizeJobSourceType(raw string) JobSourceType {
	switch strings.TrimSpace(raw) {
	case string(JobSourceGitRepo):
		return JobSourceGitRepo
	case string(JobSourceWorkspace):
		return JobSourceWorkspace
	default:
		return ""
	}
}

func JobSourceTypeLabel(sourceType JobSourceType) string {
	switch sourceType {
	case JobSourceGitRepo:
		return TaskSourceGitRepoText
	default:
		return TaskSourceWorkspaceText
	}
}

func InferJobSourceType(rawLabel, gitInput string, workspaceLinks []string) JobSourceType {
	switch strings.TrimSpace(rawLabel) {
	case TaskSourceGitRepoText, string(JobSourceGitRepo):
		return JobSourceGitRepo
	case TaskSourceWorkspaceText, string(JobSourceWorkspace):
		return JobSourceWorkspace
	}
	if strings.TrimSpace(gitInput) != "" && len(workspaceLinks) == 0 {
		return JobSourceGitRepo
	}
	return JobSourceWorkspace
}

func NormalizeJobState(job JobState) JobState {
	job.SourceType = NormalizeJobSourceType(string(job.SourceType))
	if job.SourceType == "" {
		if strings.TrimSpace(job.GitRepoURL) != "" || strings.TrimSpace(job.GitRepoSourceInput) != "" {
			job.SourceType = JobSourceGitRepo
		} else {
			job.SourceType = JobSourceWorkspace
		}
	}
	job.GitRepoSourceInput = strings.TrimSpace(job.GitRepoSourceInput)
	job.GitRepoURL = strings.TrimSpace(job.GitRepoURL)
	job.GitRef = strings.TrimSpace(job.GitRef)
	job.WorkspaceKey = strings.TrimSpace(job.WorkspaceKey)
	job.WorkspaceRecordID = strings.TrimSpace(job.WorkspaceRecordID)
	job.MaxConcurrency = DefaultMaxConcurrency(job.MaxConcurrency)
	if job.SourceType == JobSourceWorkspace {
		job.GitRepoSourceInput = ""
		job.GitRepoURL = ""
		job.GitRef = ""
	} else {
		job.WorkspaceKey = ""
		job.WorkspaceRecordID = ""
	}
	return job
}

func JobDisplaySource(job JobState) string {
	job = NormalizeJobState(job)
	if job.SourceType == JobSourceGitRepo {
		spec := cronrepo.SourceSpec{
			RawInput: job.GitRepoSourceInput,
			RepoURL:  job.GitRepoURL,
			Ref:      job.GitRef,
		}
		if strings.TrimSpace(spec.RepoURL) == "" && strings.TrimSpace(spec.RawInput) != "" {
			if parsed, err := cronrepo.ParseSourceInput(spec.RawInput); err == nil {
				spec = parsed
			}
		}
		if strings.TrimSpace(spec.RepoURL) != "" {
			return spec.SourceLabel()
		}
		return firstNonEmpty(job.GitRepoSourceInput, "repo: unknown")
	}
	return strings.TrimSpace(job.WorkspaceKey)
}

func JobConcurrencyText(limit int) string {
	return fmt.Sprintf("并发上限 %d", DefaultMaxConcurrency(limit))
}
