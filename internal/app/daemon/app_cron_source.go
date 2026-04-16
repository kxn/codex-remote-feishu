package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
)

type cronJobSourceType string

const (
	cronJobSourceWorkspace cronJobSourceType = "workspace"
	cronJobSourceGitRepo   cronJobSourceType = "git_repo"

	cronTaskSourceTypeField     = "来源类型"
	cronTaskWorkspaceField      = "工作区"
	cronTaskGitRepoInputField   = "Git 仓库引用"
	cronTaskSourceWorkspaceText = "工作区"
	cronTaskSourceGitRepoText   = "Git 仓库"
)

func normalizeCronJobSourceType(raw string) cronJobSourceType {
	switch strings.TrimSpace(raw) {
	case string(cronJobSourceGitRepo):
		return cronJobSourceGitRepo
	case string(cronJobSourceWorkspace):
		return cronJobSourceWorkspace
	default:
		return ""
	}
}

func cronJobSourceTypeLabel(sourceType cronJobSourceType) string {
	switch sourceType {
	case cronJobSourceGitRepo:
		return cronTaskSourceGitRepoText
	default:
		return cronTaskSourceWorkspaceText
	}
}

func cronInferJobSourceType(rawLabel, gitInput string, workspaceLinks []string) cronJobSourceType {
	switch strings.TrimSpace(rawLabel) {
	case cronTaskSourceGitRepoText, string(cronJobSourceGitRepo):
		return cronJobSourceGitRepo
	case cronTaskSourceWorkspaceText, string(cronJobSourceWorkspace):
		return cronJobSourceWorkspace
	}
	if strings.TrimSpace(gitInput) != "" && len(workspaceLinks) == 0 {
		return cronJobSourceGitRepo
	}
	return cronJobSourceWorkspace
}

func cronNormalizeJobState(job cronJobState) cronJobState {
	job.SourceType = normalizeCronJobSourceType(string(job.SourceType))
	if job.SourceType == "" {
		if strings.TrimSpace(job.GitRepoURL) != "" || strings.TrimSpace(job.GitRepoSourceInput) != "" {
			job.SourceType = cronJobSourceGitRepo
		} else {
			job.SourceType = cronJobSourceWorkspace
		}
	}
	job.GitRepoSourceInput = strings.TrimSpace(job.GitRepoSourceInput)
	job.GitRepoURL = strings.TrimSpace(job.GitRepoURL)
	job.GitRef = strings.TrimSpace(job.GitRef)
	job.WorkspaceKey = strings.TrimSpace(job.WorkspaceKey)
	job.WorkspaceRecordID = strings.TrimSpace(job.WorkspaceRecordID)
	if job.SourceType == cronJobSourceWorkspace {
		job.GitRepoSourceInput = ""
		job.GitRepoURL = ""
		job.GitRef = ""
	} else {
		job.WorkspaceKey = ""
		job.WorkspaceRecordID = ""
	}
	return job
}

func cronJobDisplaySource(job cronJobState) string {
	job = cronNormalizeJobState(job)
	if job.SourceType == cronJobSourceGitRepo {
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
