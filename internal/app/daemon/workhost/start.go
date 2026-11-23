package workhost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func (h *Host) startPrepared(ctx context.Context, worker work.WorkerSession, op work.Operation, record LaunchRecord, settings LaunchSettings, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(h.config.Paths.StateDir, "workers", worker.ID, fmt.Sprint(worker.Generation), "launch.json")
	if err := writeSnapshot(path, record.Snapshot); err != nil {
		return err
	}
	markSending := func(tx *workstore.Tx) error {
		role, err := tx.GetRole(worker.RoleID)
		if err != nil {
			return err
		}
		binding, err := tx.GetRoleBotBinding(worker.RoleID)
		if err != nil {
			return err
		}
		if role.SettingsRevision != record.SettingsRevision ||
			role.DescriptionVersion != record.RoleDescriptionVersion ||
			binding.IdentityGeneration != record.Snapshot.Owner.BotGeneration {
			return workstore.ErrConflict
		}
		if err := tx.CheckContextPreferencesRevision(worker.ContextID, settings.PreferencesRevision); err != nil {
			return err
		}
		if err := h.assertUpgradeLaunch(ctx, tx, worker.Backend); err != nil {
			return err
		}
		current, err := tx.GetOperation(op.ID)
		if err != nil {
			return err
		}
		if current.Revision != op.Revision || current.State != work.OperationPrepared {
			return workstore.ErrConflict
		}
		current.State, current.Revision, current.UpdatedAt = work.OperationSending, current.Revision+1, h.config.Now()
		_, _, err = tx.PutOperation(current, op.Revision)
		op = current
		return err
	}
	err := h.config.SettingsAdmission.Within(ctx, worker.RoleID, func() error { return h.store.Within(ctx, markSending) })
	if err != nil {
		return err
	}
	env := launchEnvironment(settings.Environment, map[string]string{
		config.WorkLaunchSnapshotEnv: path, config.WorkSessionTokenEnv: token,
		config.WorkTeamBinaryEnv:             h.config.BinaryPath,
		config.CodexRemoteInstanceBackendEnv: string(worker.Backend),
		"CODEX_REMOTE_INSTANCE_ID":           worker.InstanceID, "CODEX_REMOTE_INSTANCE_SOURCE": "headless",
		"CODEX_REMOTE_INSTANCE_MANAGED": "1", "CODEX_REMOTE_LIFETIME": "daemon-owned",
		"CODEX_REMOTE_INSTANCE_DISPLAY_NAME": worker.RoleID,
	})
	mode := relayruntime.HeadlessLaunchModeAppServer
	if worker.Backend == work.BackendClaude {
		mode = relayruntime.HeadlessLaunchModeClaudeAppServer
	}
	if worker.Backend == work.BackendOpenCode {
		mode = relayruntime.HeadlessLaunchModeOpenCodeACP
	}
	// Cancellation after durable sending remains an uncertain launch. Do not
	// reset it to prepared or permit a restart to send blindly.
	if err := ctx.Err(); err != nil {
		return err
	}
	pid, startErr := h.config.Start(relayruntime.HeadlessLaunchOptions{BinaryPath: h.config.BinaryPath, ConfigPath: h.config.ConfigPath, Env: env, Paths: h.config.Paths, WorkDir: record.Snapshot.CWD, InstanceID: worker.InstanceID, LaunchMode: mode, Args: settings.LaunchArgs})
	if startErr == nil && pid <= 0 {
		startErr = fmt.Errorf("host returned no process identity")
	}
	storeErr := h.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetOperation(op.ID)
		if err != nil {
			return err
		}
		if current.Revision != op.Revision {
			return workstore.ErrConflict
		}
		current.Revision++
		current.UpdatedAt = h.config.Now()
		if startErr != nil {
			current.State, current.LastError = work.OperationUnknown, startErr.Error()
		} else {
			data, _ := json.Marshal(struct {
				PID         int    `json:"pid"`
				InstanceID  string `json:"instance_id"`
				Incarnation string `json:"incarnation"`
			}{pid, worker.InstanceID, worker.CarrierIncarnation})
			current.State, current.ExternalReceipt = work.OperationConfirmed, string(data)
		}
		_, _, err = tx.PutOperation(current, op.Revision)
		return err
	})
	if storeErr != nil {
		return storeErr
	}
	return startErr
}

func writeSnapshot(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".launch-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func launchEnvironment(base []string, values map[string]string) []string {
	env := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := values[key]; replace {
			continue
		}
		if key == config.WorkOwnerBindingEnv || key == config.WorkToolEndpointEnv || key == config.WorkSessionCredentialRefEnv {
			continue
		}
		env = append(env, entry)
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}
