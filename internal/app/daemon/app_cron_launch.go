package daemon

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
)

type cronLaunchRequest struct {
	Job             cronJobState
	TriggeredAt     time.Time
	InstanceID      string
	DisplayName     string
	WritebackTarget cronWritebackTarget
	Runtime         HeadlessRuntimeConfig
	RepoManager     *cronrepo.Manager
}

type cronPreparedRun struct {
	Run cronRunState
	Env []string
}

func (a *App) newCronLaunchRequestLocked(job cronJobState, now time.Time) cronLaunchRequest {
	runtimeSnapshot := a.headlessRuntime
	runtimeSnapshot.BaseEnv = append([]string(nil), a.headlessRuntime.BaseEnv...)
	runtimeSnapshot.LaunchArgs = append([]string(nil), a.headlessRuntime.LaunchArgs...)
	return cronLaunchRequest{
		Job:             cronNormalizeJobState(job),
		TriggeredAt:     now,
		InstanceID:      cronInstanceIDForRun(job.RecordID, now),
		DisplayName:     firstNonEmpty(strings.TrimSpace(job.Name), "cron"),
		WritebackTarget: a.snapshotCronWritebackLocked(),
		Runtime:         runtimeSnapshot,
		RepoManager:     a.cronRepoManagerLocked(),
	}
}

func (a *App) cronRepoManagerLocked() *cronrepo.Manager {
	if a.cronRepoManager == nil {
		a.cronRepoManager = cronrepo.NewManager(a.headlessRuntime.Paths.StateDir)
	}
	return a.cronRepoManager
}

func (a *App) launchCronRequestsLocked(requests []cronLaunchRequest) {
	for _, request := range requests {
		a.mu.Unlock()
		prepared, err := a.prepareCronRunLaunch(request)
		a.mu.Lock()
		if err != nil {
			a.recordCronImmediateResultWithTargetLocked(request.WritebackTarget, request.Job, request.TriggeredAt, "failed", err.Error())
			continue
		}
		run := prepared.Run
		a.cronRuns[run.InstanceID] = &run
		a.cronJobActiveRuns[cronJobActiveKey(run.JobRecordID, run.JobName)] = run.InstanceID
		delete(a.cronExitTargets, run.InstanceID)

		a.mu.Unlock()
		pid, launchErr := a.startHeadless(controlToHeadlessLaunch(request.Runtime, prepared.Env, run.RunDirectory, run.InstanceID))
		a.mu.Lock()

		existing := a.cronRuns[run.InstanceID]
		if launchErr != nil {
			delete(a.cronRuns, run.InstanceID)
			if activeKey := cronJobActiveKey(run.JobRecordID, run.JobName); activeKey != "" {
				if activeInstanceID := strings.TrimSpace(a.cronJobActiveRuns[activeKey]); activeInstanceID == run.InstanceID {
					delete(a.cronJobActiveRuns, activeKey)
				}
			}
			a.mu.Unlock()
			a.cleanupCronRunResources(run)
			a.mu.Lock()
			a.recordCronImmediateResultWithTargetLocked(request.WritebackTarget, request.Job, request.TriggeredAt, "failed", fmt.Sprintf("启动隐藏执行失败：%v", launchErr))
			continue
		}
		if existing == nil {
			a.mu.Unlock()
			a.cleanupCronRunResources(run)
			a.mu.Lock()
			continue
		}
		if existing.PID <= 0 {
			existing.PID = pid
		}
		log.Printf("cron hidden run requested: instance=%s job=%s source=%s cwd=%s pid=%d", existing.InstanceID, existing.JobName, existing.SourceLabel, existing.RunDirectory, existing.PID)
	}
}

func (a *App) prepareCronRunLaunch(request cronLaunchRequest) (cronPreparedRun, error) {
	cfg := request.Runtime
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		return cronPreparedRun{}, fmt.Errorf("headless binary 未配置，无法执行 Cron 任务")
	}
	job := cronNormalizeJobState(request.Job)

	runDirectory := ""
	runRoot := ""
	sourceLabel := cronJobDisplaySource(job)
	gitSourceKey := ""
	gitRepoURL := ""
	gitRef := ""
	switch job.SourceType {
	case cronJobSourceGitRepo:
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
		Run: cronRunState{
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
			TimeoutMinutes:  cronDefaultTimeoutMinutes(job.TimeoutMinutes),
			TriggeredAt:     request.TriggeredAt,
			Status:          "starting",
			Buffers:         map[string]*cronItemBuffer{},
		},
		Env: env,
	}, nil
}

func (a *App) recordCronImmediateResultWithTargetLocked(target cronWritebackTarget, job cronJobState, triggeredAt time.Time, status, errorMessage string) {
	if !target.valid() {
		log.Printf("cron immediate result skipped: no writeback target job=%s status=%s", job.Name, status)
		return
	}
	job = cronNormalizeJobState(job)
	run := cronRunState{
		RunID:           cronInstanceIDForRun(job.RecordID, triggeredAt),
		InstanceID:      cronInstanceIDForRun(job.RecordID, triggeredAt),
		GatewayID:       target.GatewayID,
		WritebackTarget: target,
		JobRecordID:     strings.TrimSpace(job.RecordID),
		JobName:         firstNonEmpty(strings.TrimSpace(job.Name), strings.TrimSpace(job.RecordID)),
		SourceType:      job.SourceType,
		SourceLabel:     cronJobDisplaySource(job),
		WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
		GitRepoURL:      strings.TrimSpace(job.GitRepoURL),
		GitRef:          strings.TrimSpace(job.GitRef),
		Prompt:          strings.TrimSpace(job.Prompt),
		TimeoutMinutes:  cronDefaultTimeoutMinutes(job.TimeoutMinutes),
		TriggeredAt:     triggeredAt,
		CompletedAt:     triggeredAt,
		Status:          strings.TrimSpace(status),
		ErrorMessage:    strings.TrimSpace(errorMessage),
	}
	go a.writeCronRunResultAsync(target, run)
}

func (a *App) cleanupCronRunResources(run cronRunState) {
	run = cronRunState{
		SourceType:   run.SourceType,
		RunRoot:      strings.TrimSpace(run.RunRoot),
		RunDirectory: strings.TrimSpace(run.RunDirectory),
		GitSourceKey: strings.TrimSpace(run.GitSourceKey),
	}
	if run.SourceType != cronJobSourceGitRepo || run.RunRoot == "" || run.GitSourceKey == "" {
		return
	}
	manager := a.cronRepoManager
	if manager == nil {
		manager = cronrepo.NewManager(a.headlessRuntime.Paths.StateDir)
	}
	if err := manager.CleanupRun(context.Background(), run.GitSourceKey, run.RunRoot); err != nil {
		log.Printf("cron run cleanup failed: root=%s err=%v", filepath.Clean(run.RunRoot), err)
	}
}
