package daemon

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
)

type cronLaunchRequest struct {
	Job             cronrt.JobState
	TriggeredAt     time.Time
	InstanceID      string
	DisplayName     string
	WritebackTarget cronrt.WritebackTarget
	Runtime         HeadlessRuntimeConfig
	RepoManager     *cronrepo.Manager
}

type cronPreparedRun struct {
	Run cronrt.RunState
	Env []string
}

func (a *App) newCronLaunchRequestLocked(job cronrt.JobState, now time.Time) cronLaunchRequest {
	runtimeSnapshot := a.headlessRuntime
	runtimeSnapshot.BaseEnv = append([]string(nil), a.headlessRuntime.BaseEnv...)
	runtimeSnapshot.LaunchArgs = append([]string(nil), a.headlessRuntime.LaunchArgs...)
	return cronLaunchRequest{
		Job:             cronrt.NormalizeJobState(job),
		TriggeredAt:     now,
		InstanceID:      cronrt.InstanceIDForRun(job.RecordID, now),
		DisplayName:     firstNonEmpty(strings.TrimSpace(job.Name), "cron"),
		WritebackTarget: a.snapshotCronWritebackLocked(),
		Runtime:         runtimeSnapshot,
		RepoManager:     a.cronRepoManagerLocked(),
	}
}

func (a *App) cronRepoManagerLocked() *cronrepo.Manager {
	if a.cronRuntime.repoManager == nil {
		a.cronRuntime.repoManager = cronrepo.NewManager(a.headlessRuntime.Paths.StateDir)
	}
	return a.cronRuntime.repoManager
}

func (a *App) launchCronRequestsLocked(requests []cronLaunchRequest) {
	for _, request := range requests {
		if err := a.launchCronRequestLocked(request); err != nil {
			a.recordCronImmediateResultWithTargetLocked(request.WritebackTarget, request.Job, request.TriggeredAt, "failed", err.Error())
		}
	}
}

func (a *App) launchCronRequestLocked(request cronLaunchRequest) error {
	a.mu.Unlock()
	prepared, err := a.prepareCronRunLaunch(request)
	a.mu.Lock()
	if err != nil {
		return err
	}
	run := prepared.Run
	a.cronRuntime.runs[run.InstanceID] = &run
	a.addCronActiveRunLocked(run.JobRecordID, run.JobName, run.InstanceID)
	delete(a.cronRuntime.exitTargets, run.InstanceID)

	a.mu.Unlock()
	pid, launchErr := a.startHeadless(controlToHeadlessLaunch(request.Runtime, prepared.Env, run.RunDirectory, run.InstanceID))
	a.mu.Lock()

	existing := a.cronRuntime.runs[run.InstanceID]
	if launchErr != nil {
		delete(a.cronRuntime.runs, run.InstanceID)
		a.removeCronActiveRunLocked(run.JobRecordID, run.JobName, run.InstanceID)
		a.mu.Unlock()
		a.cleanupCronRunResources(run)
		a.mu.Lock()
		return fmt.Errorf("启动隐藏执行失败：%v", launchErr)
	}
	if existing == nil {
		a.mu.Unlock()
		a.cleanupCronRunResources(run)
		a.mu.Lock()
		return nil
	}
	if existing.PID <= 0 {
		existing.PID = pid
	}
	log.Printf("cron hidden run requested: instance=%s job=%s source=%s cwd=%s pid=%d", existing.InstanceID, existing.JobName, existing.SourceLabel, existing.RunDirectory, existing.PID)
	return nil
}

func (a *App) prepareCronRunLaunch(request cronLaunchRequest) (cronPreparedRun, error) {
	cfg := request.Runtime
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return cronPreparedRun{}, fmt.Errorf("headless binary 未配置，无法执行 Cron 任务")
	}
	job := cronrt.NormalizeJobState(request.Job)

	runDirectory := ""
	runRoot := ""
	sourceLabel := cronrt.JobDisplaySource(job)
	gitSourceKey := ""
	gitRepoURL := ""
	gitRef := ""
	switch job.SourceType {
	case cronrt.JobSourceGitRepo:
		spec := cronrepo.SourceSpec{
			RawInput: job.GitRepoSourceInput,
			RepoURL:  job.GitRepoURL,
			Ref:      job.GitRef,
		}
		result, err := request.RepoManager.Materialize(context.Background(), request.InstanceID, spec)
		if err != nil {
			return cronPreparedRun{}, err
		}
		runDirectory = result.RunDirectory
		runRoot = result.RunRoot
		sourceLabel = result.SourceLabel
		gitSourceKey = result.SourceKey
		gitRepoURL = result.SourceSpec.RepoURL
		gitRef = result.ResolvedRef
	default:
		workspaceRoot, err := normalizeWorkspaceRoot(job.WorkspaceKey)
		if err != nil {
			return cronPreparedRun{}, fmt.Errorf("工作区不可用：%w", err)
		}
		runDirectory = workspaceRoot
		sourceLabel = firstNonEmpty(sourceLabel, workspaceRoot)
	}

	env := append([]string{}, cfg.BaseEnv...)
	env = append(env,
		"CODEX_REMOTE_INSTANCE_ID="+request.InstanceID,
		"CODEX_REMOTE_INSTANCE_SOURCE=cron",
		"CODEX_REMOTE_LIFETIME=daemon-owned",
		"CODEX_REMOTE_INSTANCE_DISPLAY_NAME=cron:"+request.DisplayName,
	)
	return cronPreparedRun{
		Run: cronrt.RunState{
			RunID:           request.InstanceID,
			InstanceID:      request.InstanceID,
			GatewayID:       strings.TrimSpace(request.WritebackTarget.GatewayID),
			WritebackTarget: request.WritebackTarget,
			JobRecordID:     strings.TrimSpace(job.RecordID),
			JobName:         firstNonEmpty(strings.TrimSpace(job.Name), request.DisplayName),
			SourceType:      job.SourceType,
			SourceLabel:     sourceLabel,
			WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
			RunRoot:         runRoot,
			RunDirectory:    runDirectory,
			GitSourceKey:    gitSourceKey,
			GitRepoURL:      gitRepoURL,
			GitRef:          gitRef,
			Prompt:          strings.TrimSpace(job.Prompt),
			TimeoutMinutes:  cronrt.DefaultTimeoutMinutes(job.TimeoutMinutes),
			TriggeredAt:     request.TriggeredAt,
			Status:          "starting",
			Buffers:         map[string]*cronrt.ItemBuffer{},
		},
		Env: env,
	}, nil
}

func (a *App) recordCronImmediateResultWithTargetLocked(target cronrt.WritebackTarget, job cronrt.JobState, triggeredAt time.Time, status, errorMessage string) {
	if !target.Valid() {
		log.Printf("cron immediate result skipped: no writeback target job=%s status=%s", job.Name, status)
		return
	}
	job = cronrt.NormalizeJobState(job)
	run := cronrt.RunState{
		RunID:           cronrt.InstanceIDForRun(job.RecordID, triggeredAt),
		InstanceID:      cronrt.InstanceIDForRun(job.RecordID, triggeredAt),
		GatewayID:       target.GatewayID,
		WritebackTarget: target,
		JobRecordID:     strings.TrimSpace(job.RecordID),
		JobName:         firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID)),
		SourceType:      job.SourceType,
		SourceLabel:     cronrt.JobDisplaySource(job),
		WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
		GitRepoURL:      strings.TrimSpace(job.GitRepoURL),
		GitRef:          strings.TrimSpace(job.GitRef),
		Prompt:          strings.TrimSpace(job.Prompt),
		TimeoutMinutes:  cronrt.DefaultTimeoutMinutes(job.TimeoutMinutes),
		TriggeredAt:     triggeredAt,
		CompletedAt:     triggeredAt,
		Status:          strings.TrimSpace(status),
		ErrorMessage:    strings.TrimSpace(errorMessage),
	}
	go a.writeCronRunResultAsync(target, run)
}

func (a *App) cleanupCronRunResources(run cronrt.RunState) {
	run = cronrt.RunState{
		SourceType:   run.SourceType,
		RunRoot:      strings.TrimSpace(run.RunRoot),
		RunDirectory: strings.TrimSpace(run.RunDirectory),
		GitSourceKey: strings.TrimSpace(run.GitSourceKey),
	}
	if run.SourceType != cronrt.JobSourceGitRepo || run.RunRoot == "" || run.GitSourceKey == "" {
		return
	}
	manager := a.cronRuntime.repoManager
	if manager == nil {
		manager = cronrepo.NewManager(a.headlessRuntime.Paths.StateDir)
	}
	if err := manager.CleanupRun(context.Background(), run.GitSourceKey, run.RunRoot); err != nil {
		log.Printf("cron run cleanup failed: root=%s err=%v", filepath.Clean(run.RunRoot), err)
	}
}
