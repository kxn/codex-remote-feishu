package wrapper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const desktopPredictionSetting = "composer-predictions-enabled"

type predictionDesktopPreferences struct {
	owner  *bool
	legacy *bool
}

func (r *codexBackendRuntime) predictionEnabledForJob(job *predictionJob, identity predictionIdentity) bool {
	preferences, err := r.predictions.desktopPreferences(job.ctx, identity)
	if err != nil {
		return false
	}
	if preferences.owner != nil {
		return *preferences.owner
	}
	result, err := r.predictionRPC(job.ctx, job.frames, "config/read", map[string]any{"includeLayers": false})
	if err != nil {
		return false
	}
	var response struct {
		Config *struct {
			Desktop map[string]json.RawMessage `json:"desktop"`
		} `json:"config"`
	}
	if json.Unmarshal(result, &response) != nil || response.Config == nil {
		return false
	}
	if enabled := desktopPredictionBoolean(response.Config.Desktop[desktopPredictionSetting]); enabled != nil {
		return *enabled
	}
	if preferences.legacy != nil {
		return *preferences.legacy
	}
	return true // Desktop setting registry default.
}

func readPredictionDesktopPreferences(ctx context.Context, principal predictionIdentity) (predictionDesktopPreferences, error) {
	if err := ctx.Err(); err != nil {
		return predictionDesktopPreferences{}, err
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return predictionDesktopPreferences{}, err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return readPredictionDesktopPreferencesAt(codexHome, principal)
}

func readPredictionDesktopPreferencesAt(codexHome string, principal predictionIdentity) (predictionDesktopPreferences, error) {
	// Desktop can migrate its state to native storage. The JSON file must not
	// override an active choice we cannot verify in native storage.
	if _, err := os.Stat(filepath.Join(codexHome, ".codex-global-state.sqlite")); !errors.Is(err, os.ErrNotExist) {
		return predictionDesktopPreferences{}, errors.New("desktop prediction preferences unavailable in native storage")
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, ".codex-global-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return predictionDesktopPreferences{}, nil
	}
	if err != nil {
		return predictionDesktopPreferences{}, err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		return predictionDesktopPreferences{}, err
	}
	if marker := state["electron-renderer-content-owner"]; len(marker) != 0 && string(marker) != "null" {
		var activeOwner string
		tuple, _ := json.Marshal([]string{principal.UserID, principal.AccountID})
		hash := sha256.Sum256(tuple)
		if json.Unmarshal(marker, &activeOwner) != nil || activeOwner != hex.EncodeToString(hash[:]) {
			return predictionDesktopPreferences{}, errors.New("desktop prediction settings belong to another principal")
		}
		if raw := state["electron-owner-atoms:"+activeOwner]; len(raw) != 0 {
			var atoms map[string]json.RawMessage
			if err := json.Unmarshal(raw, &atoms); err != nil {
				return predictionDesktopPreferences{}, err
			}
			if value, exists := atoms[desktopPredictionSetting]; exists {
				return predictionDesktopPreferences{owner: desktopPredictionBoolean(value)}, nil
			}
		}
	}
	var legacy map[string]json.RawMessage
	if raw := state["electron-persisted-atom-state"]; len(raw) != 0 {
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return predictionDesktopPreferences{}, err
		}
	}
	return predictionDesktopPreferences{legacy: desktopPredictionBoolean(legacy[desktopPredictionSetting])}, nil
}

func desktopPredictionBoolean(raw json.RawMessage) *bool {
	var enabled *bool
	if json.Unmarshal(raw, &enabled) != nil {
		return nil
	}
	return enabled
}
