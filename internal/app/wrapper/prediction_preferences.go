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
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const desktopPredictionSetting = "composer-predictions-enabled"

type predictionDesktopPreferences struct {
	owner  *bool
	legacy *bool
}

type predictionPreferenceState struct {
	principal    predictionIdentity
	configured   *bool
	requireOwner bool
}

func (p predictionPreferenceState) allows(pref predictionDesktopPreferences) bool {
	if pref.owner != nil {
		return *pref.owner
	}
	if p.requireOwner {
		return false // A known owner value disappeared and we did not read its fallback.
	}
	if p.configured != nil {
		return *p.configured
	}
	if pref.legacy != nil {
		return *pref.legacy
	}
	return true // Desktop setting registry default.
}

func (r *codexBackendRuntime) predictionPreferencesForJob(job *predictionJob, identity predictionIdentity) (*predictionPreferenceState, bool) {
	preferences, err := r.predictions.desktopPreferences(job.ctx, identity)
	if err != nil {
		return nil, false
	}
	state := &predictionPreferenceState{principal: identity}
	if preferences.owner != nil {
		state.requireOwner = true
		return state, state.allows(preferences)
	}
	result, err := r.predictionRPC(job.ctx, job.frames, "config/read", map[string]any{"includeLayers": false})
	if err != nil {
		return nil, false
	}
	var response struct {
		Config *struct {
			Desktop map[string]json.RawMessage `json:"desktop"`
		} `json:"config"`
	}
	if json.Unmarshal(result, &response) != nil || response.Config == nil {
		return nil, false
	}
	state.configured = desktopPredictionBoolean(response.Config.Desktop[desktopPredictionSetting])
	return state, state.allows(preferences)
}

func (r *codexBackendRuntime) desktopPredictionAllowed(ctx context.Context, state *predictionPreferenceState) bool {
	if state == nil {
		return true
	}
	preferences, err := r.predictions.desktopPreferences(ctx, state.principal)
	return err == nil && state.allows(preferences)
}

type predictionPreferenceWatch struct {
	stop        context.CancelFunc
	invalidated bool
	published   bool
}

// Desktop clears an in-flight or published prediction when its setting changes.
// Watch the persisted atom while a relay-visible suggestion may exist. Desktop
// loads the native config fallback into SettingsStore at startup, so polling
// for edits to the native config would introduce a different live behavior.
func (r *codexBackendRuntime) watchPredictionPreferenceLocked(relayCtx context.Context, preference *predictionPreferenceState, sender relayEventSender) {
	previous := r.predictions.preferenceWatch
	ctx, cancel := context.WithCancel(relayCtx)
	watch := &predictionPreferenceWatch{stop: cancel, published: previous != nil && previous.published}
	r.stopPredictionPreferenceWatchLocked()
	r.predictions.preferenceWatch = watch
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !watch.invalidated && r.desktopPredictionAllowed(ctx, preference) {
				continue
			}
			r.mu.Lock()
			if r.predictions.preferenceWatch != watch || ctx.Err() != nil {
				r.mu.Unlock()
				return
			}
			if !watch.invalidated {
				r.cancelPredictionJobsLocked()
				watch.invalidated = true
			}
			// SendEvents enqueues to the bounded relay outbox. Keep the notification
			// ordered before later samples, and retry if the connection queues are full.
			err := sender.SendEvents([]agentproto.Event{predictionPreferenceInvalidated()})
			if err == nil {
				r.stopPredictionPreferenceWatchLocked()
			}
			r.mu.Unlock()
			if err == nil {
				return
			}
		}
	}()
}

func (r *codexBackendRuntime) stopPredictionPreferenceWatchLocked() {
	if r.predictions != nil && r.predictions.preferenceWatch != nil {
		r.predictions.preferenceWatch.stop()
		r.predictions.preferenceWatch = nil
	}
}

func (r *codexBackendRuntime) stopIdlePredictionPreferenceWatchLocked() {
	watch := r.predictions.preferenceWatch
	if watch == nil || watch.published || watch.invalidated {
		return
	}
	for _, job := range r.predictions.jobs {
		if job.sampled {
			return
		}
	}
	r.stopPredictionPreferenceWatchLocked()
}

func (r *codexBackendRuntime) endPredictionJobLocked(job *predictionJob, published bool) {
	job.cancel()
	delete(r.predictions.jobs, job.command.CommandID)
	if watch := r.predictions.preferenceWatch; watch != nil && published {
		watch.published = true
	}
	r.stopIdlePredictionPreferenceWatchLocked()
}

func predictionHasSuggestion(event agentproto.Event) bool {
	return event.Prediction != nil && event.Prediction.Suggestion != ""
}

func (r *codexBackendRuntime) predictionResultAllowedLocked(job *predictionJob, event agentproto.Event) bool {
	return !predictionHasSuggestion(event) || r.desktopPredictionAllowed(job.ctx, job.preference)
}

func (r *codexBackendRuntime) invalidatePredictionPreferenceLocked() agentproto.Event {
	r.cancelPredictionJobsLocked()
	r.stopPredictionPreferenceWatchLocked()
	return predictionPreferenceInvalidated()
}

func predictionPreferenceInvalidated() agentproto.Event {
	return agentproto.Event{Kind: agentproto.EventThreadPredictionInvalidated}
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
