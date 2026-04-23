package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
)

func (a *App) cronStatePath() string {
	if strings.TrimSpace(a.headlessRuntime.Paths.StateDir) != "" {
		return filepath.Join(a.headlessRuntime.Paths.StateDir, "cron-state.json")
	}
	return filepath.Join(".", "cron-state.json")
}

func (a *App) loadCronStateLocked(create bool) (*cronrt.StateFile, error) {
	if a.cronRuntime.loaded {
		if err := a.normalizeLoadedCronStateLocked(); err != nil {
			return nil, err
		}
		if a.cronRuntime.state == nil && create {
			stateValue, err := a.newCronStateLocked()
			if err != nil {
				return nil, err
			}
			a.cronRuntime.state = stateValue
			if err := a.writeCronStateLocked(); err != nil {
				return nil, err
			}
		}
		return a.cronRuntime.state, nil
	}
	path := a.cronStatePath()
	a.cronRuntime.stateIOMu.Lock()
	a.mu.Unlock()
	raw, err := os.ReadFile(path)
	a.cronRuntime.stateIOMu.Unlock()
	a.mu.Lock()
	if a.cronRuntime.loaded {
		if err := a.normalizeLoadedCronStateLocked(); err != nil {
			return nil, err
		}
		if a.cronRuntime.state == nil && create {
			stateValue, createErr := a.newCronStateLocked()
			if createErr != nil {
				return nil, createErr
			}
			a.cronRuntime.state = stateValue
			if err := a.writeCronStateLocked(); err != nil {
				return nil, err
			}
		}
		return a.cronRuntime.state, nil
	}
	switch {
	case os.IsNotExist(err):
		a.cronRuntime.loaded = true
		if !create {
			return nil, nil
		}
		stateValue, createErr := a.newCronStateLocked()
		if createErr != nil {
			return nil, createErr
		}
		a.cronRuntime.state = stateValue
		if err := a.writeCronStateLocked(); err != nil {
			return nil, err
		}
		return a.cronRuntime.state, nil
	case err != nil:
		return nil, err
	}
	var stateValue cronrt.StateFile
	if err := json.Unmarshal(raw, &stateValue); err != nil {
		return nil, err
	}
	a.cronRuntime.loaded = true
	a.cronRuntime.state = cronrt.NormalizeState(stateValue)
	if err := a.normalizeLoadedCronStateLocked(); err != nil {
		return nil, err
	}
	return a.cronRuntime.state, nil
}

func (a *App) normalizeLoadedCronStateLocked() error {
	if a.cronRuntime.state == nil {
		return nil
	}
	a.cronRuntime.state = cronrt.NormalizeState(*a.cronRuntime.state)
	changed, err := a.migrateCronLegacyOwnerStateLocked(a.cronRuntime.state)
	if err != nil {
		return nil
	}
	if !changed {
		return nil
	}
	return a.writeCronStateLocked()
}

func (a *App) newCronStateLocked() (*cronrt.StateFile, error) {
	scopeKey, label, err := a.cronInstanceMetadataLocked()
	if err != nil {
		return nil, err
	}
	return cronrt.NewState(scopeKey, label), nil
}

func (a *App) writeCronStateLocked() error {
	if a.cronRuntime.state == nil {
		return nil
	}
	updatedAt := time.Now().UTC()
	a.cronRuntime.state.SchemaVersion = cronrt.StateSchemaVersion
	a.cronRuntime.state.UpdatedAt = updatedAt
	snapshot := cronrt.CloneState(a.cronRuntime.state)
	if snapshot == nil {
		return nil
	}
	path := a.cronStatePath()
	a.cronRuntime.stateIOMu.Lock()
	a.mu.Unlock()
	err := writeJSONFileAtomic(path, snapshot, 0o600)
	a.cronRuntime.stateIOMu.Unlock()
	a.mu.Lock()
	return err
}

func (a *App) cronInstanceMetadataLocked() (string, string, error) {
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return "", "", err
	}
	instanceID := strings.TrimSpace(stateValue.InstanceID)
	if instanceID == "" {
		instanceID = cronrt.FallbackInstanceID(stateValue.ConfigPath, stateValue.StatePath)
	}
	return instanceID, instanceID, nil
}
