package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

var privateRunLocalUpgrade = install.RunLocalBinaryUpgradeWithStatePath

func privateLocalArtifactHash(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return "", fmt.Errorf("本地升级产物不可用")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", fmt.Errorf("本地产物已变化")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func runPrivateUpgradeLocal(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand) ([]privatechat.EffectRecord, error) {
	reducer := privateUpgradeCheckReducer{host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, privatechat.ErrJournalState
	}
	if record.State == "prepared" {
		if err := j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
	}
	host.mu.Lock()
	state, _, err := host.loadUpgradeStateLocked(true)
	if err == nil && (host.upgradeRuntime.CheckInFlight || host.upgradeRuntime.StartInFlight || pendingUpgradeBusy(state.PendingUpgrade)) {
		err = fmt.Errorf("upgrade transaction already active")
	}
	host.mu.Unlock()
	if err != nil {
		return retryPrivateUpgradeCheck(ctx, j, o, generation, record, err)
	}
	result := privateUpgradeCheckReceipt{Version: 1, EffectID: record.Effect.ID}
	var event eventcontract.Event
	if !install.CurrentBuildAllowsLocalUpgrade() {
		event = upgradeNoticeEvent(o.SurfaceID, "upgrade_local_unsupported", "当前构建不支持本地升级。")
	} else {
		artifact := install.LocalUpgradeArtifactPath(state)
		hash, e := privateLocalArtifactHash(artifact)
		if e != nil {
			event = upgradeNoticeEvent(o.SurfaceID, "upgrade_local_artifact_missing", fmt.Sprintf("本地升级产物不可用：%s", artifact))
		} else {
			now := time.Now().UTC()
			messageID := ""
			if cmd.FromCardAction {
				messageID = cmd.SourceMessageID
			}
			flow := &orchestrator.PrivateUpgradeFlow{ID: "private-upgrade-" + record.Effect.ID, OwnerUserID: o.UserID, MessageID: messageID, Phase: "running", Source: string(install.UpgradeSourceLocal), Track: string(state.CurrentTrack), CurrentVersion: state.CurrentVersion, TargetVersion: "本地构建", StateFingerprint: install.UpgradeStateFingerprint(state), SourceSHA256: hash, CreatedAt: now, ExpiresAt: now.Add(upgradeOwnerFlowTTL)}
			result.Flow = flow
			result.Execute = true
			view := &upgraderuntime.OwnerCardFlowRecord{FlowID: flow.ID, SurfaceSessionID: o.SurfaceID, OwnerUserID: o.UserID, MessageID: messageID, Stage: upgraderuntime.OwnerFlowStageRunning, Source: install.UpgradeSourceLocal, Track: state.CurrentTrack, CurrentVersion: state.CurrentVersion, TargetVersion: flow.TargetVersion, CreatedAt: now, ExpiresAt: flow.ExpiresAt}
			event = upgradeOwnerRunningEvent(o.SurfaceID, view, "正在准备本地升级", "正在校验并准备固定路径的本地产物。", false)
		}
	}
	event.GatewayID = o.GatewayID
	event.SurfaceSessionID = o.SurfaceID
	result.Event, err = privatechat.EncodeEffectEvent(event)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
}

// freezePrivateLocalArtifact makes a private immutable copy and verifies the
// exact bytes approved in the owner checkpoint before invoking the installer.
func freezePrivateLocalArtifact(state install.InstallState, flow *orchestrator.PrivateUpgradeFlow) (string, error) {
	source := install.LocalUpgradeArtifactPath(state)
	f, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("本地产物不可用")
	}
	dir := filepath.Join(filepath.Dir(state.StatePath), "private-local-upgrade")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, "artifact-*")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	ok := false
	defer func() {
		temp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err = temp.Chmod(0700); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err = io.Copy(io.MultiWriter(temp, h), f); err != nil {
		return "", err
	}
	if err = temp.Sync(); err != nil {
		return "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != flow.SourceSHA256 {
		return "", fmt.Errorf("本地产物已变化，请重新执行升级")
	}
	if err = temp.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
