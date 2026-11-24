package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
)

func (a *App) startProfileCatalogRefresh(ctx context.Context, target teamProfileCatalogTarget) (workstore.ProfileModelCatalog, error) {
	finished, admitted := a.beginApplicationNativeDelivery()
	if !admitted {
		return workstore.ProfileModelCatalog{}, errApplicationClosing
	}
	async := false
	defer func() {
		if !async {
			finished()
		}
	}()
	r := a.teamRuntime
	a.profileCatalogMu.Lock()
	defer a.profileCatalogMu.Unlock()
	if a.profileCatalogClosed {
		return workstore.ProfileModelCatalog{}, errApplicationClosing
	}
	var record workstore.ProfileModelCatalog
	load := func() error {
		return r.store.Within(ctx, func(tx *workstore.Tx) error { var e error; record, e = tx.GetProfileModelCatalog(target.Key); return e })
	}
	if a.profileCatalogPending[target.Key] {
		err := load()
		return record, err
	}
	if len(a.profileCatalogPending) >= 2 {
		err := load()
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return record, err
		}
		if record.Refreshing {
			return record, nil
		}
		queued := workui.ID("profile-catalog-queued", target.Key, time.Now().UTC().Format(time.RFC3339Nano))
		err = r.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			record, e = tx.BeginProfileModelCatalogRefresh(target.Key, queued, time.Now().UTC())
			return e
		})
		return record, err
	}

	attempt := workui.ID("profile-catalog-attempt", target.Key, time.Now().UTC().Format(time.RFC3339Nano))
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		record, e = tx.BeginProfileModelCatalogRefresh(target.Key, attempt, time.Now().UTC())
		return e
	})
	if err != nil {
		return record, err
	}
	if a.profileCatalogPending == nil {
		a.profileCatalogPending = map[string]bool{}
	}
	a.profileCatalogPending[target.Key] = true
	if a.profileCatalogContext == nil {
		a.profileCatalogContext, a.profileCatalogCancel = context.WithCancel(context.WithoutCancel(ctx))
	}
	workerContext := a.profileCatalogContext
	a.profileCatalogWorkers.Add(1)
	async = true
	go func() {
		defer finished()
		defer a.profileCatalogWorkers.Done()
		defer func() {
			a.profileCatalogMu.Lock()
			delete(a.profileCatalogPending, target.Key)
			a.profileCatalogMu.Unlock()
		}()
		probeCtx, cancel := context.WithTimeout(workerContext, 35*time.Second)
		defer cancel()
		run := a.runCodexModelCatalogProbe
		if run == nil {
			run = codexprofile.RunModelCatalogProbe
		}
		var observation codexprofile.ModelCatalogProbeObservation
		problem := codexprofile.EnsureLaunchManagedFiles(target.Launch)
		if problem == nil {
			observation, problem = run(probeCtx, target.Options)
		}
		if problem == nil && target.NativeGuard != "" {
			guard, e := nativeCatalogCredentialGuard(target.NativeEnv, target.NativeProviderKeys)
			if e != nil || guard != target.NativeGuard {
				problem = fmt.Errorf("原生登录信息在查询期间发生变化，请刷新重试")
			}
		}
		if problem == nil && (observation.Snapshot.Unsupported || observation.Snapshot.ErrorMessage != "") {
			problem = fmt.Errorf("模型服务未能返回可用的模型列表")
		}
		// Shutdown cancellation is not a failed lookup. Leave the durable
		// refresh marker in place so the next host retries immediately rather
		// than treating an interrupted observation as a five-minute failure.
		a.profileCatalogMu.Lock()
		defer a.profileCatalogMu.Unlock()
		if errors.Is(workerContext.Err(), context.Canceled) {
			return
		}
		// This is read-only discovery, so an interrupted attempt can safely be
		// retried. Completion only replaces this namespace's registered attempt.
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer saveCancel()
		err := r.store.Within(saveCtx, func(tx *workstore.Tx) error {
			reason := ""
			if problem != nil {
				reason = profileCatalogError(problem)
			}
			_, e := tx.CompleteProfileModelCatalogRefresh(target.Key, attempt, &observation.Snapshot, observation.EvidenceID, reason, time.Now().UTC())
			return e
		})
		if err != nil {
			log.Printf("profile catalog completion failed: %v", err)
		}
	}()
	return record, nil
}

func profileCatalogError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "读取模型列表超时，请刷新重试。"
	}
	if errors.Is(err, context.Canceled) {
		return "读取模型列表已中断，请刷新重试。"
	}
	code, stage := codexprofile.OAuthProbeErrorCode(err), codexprofile.OAuthProbeErrorStage(err)
	switch {
	case strings.Contains(stage, "account") || code == codexprofile.ErrorOAuthMissing || code == codexprofile.ErrorOAuthProbeUnknown && stage == "":
		return "无法确认当前登录账号，请检查 ChatGPT 登录状态后刷新。"
	case code == codexprofile.ErrorCodexProbeTimeout:
		return "读取模型列表超时，请刷新重试。"
	case strings.Contains(stage, "launch") || code == codexprofile.ErrorCodexBinaryUnavailable:
		return "无法启动 Codex 模型查询进程，请检查 Codex 安装后刷新。"
	case code == codexprofile.ErrorCodexCapabilityUnsupported:
		return "当前 Codex 版本不支持读取模型列表，请更新 Codex。"
	case code == codexprofile.ErrorCodexProbeContractMismatch:
		return "Codex 返回的模型列表格式无法识别，请检查 Codex 版本。"
	case code != "":
		return "Codex 未能完成本次模型查询，请检查登录或服务连接后刷新。"
	default:
		return "读取模型列表失败：" + teamStatusPreview(err.Error(), 180)
	}
}

// Stop shared discovery before its durable store can be closed. Holding the
// admission mutex while closing prevents a concurrent Add after Wait starts.
func (a *App) stopProfileCatalogWorkers(ctx context.Context) error {
	a.profileCatalogMu.Lock()
	a.profileCatalogClosed = true
	if a.profileCatalogCancel != nil {
		a.profileCatalogCancel()
	}
	a.profileCatalogMu.Unlock()
	done := make(chan struct{})
	go func() { a.profileCatalogWorkers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("profile catalog discovery did not stop: %w", ctx.Err())
	}
}
