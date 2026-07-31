package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishubotidentity"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type persistedStateDiagnostic struct {
	status       persistedStoreStatus
	path         string
	err          error
	storePresent bool
}

type persistedStateHarness struct {
	name       string
	statePath  func(string) string
	configure  func(*App, string)
	seed       func(*App)
	mutate     func(*App)
	sync       func(*App)
	degrade    func(*App, error)
	diagnostic func(*App) persistedStateDiagnostic
}

func persistedStateHarnesses() []persistedStateHarness {
	return []persistedStateHarness{
		{
			name:      "surface resume",
			statePath: surfaceresume.StatePath,
			configure: func(app *App, stateDir string) {
				app.configureSurfaceResumeStateLocked(stateDir)
			},
			seed: func(app *App) {
				app.service.MaterializeSurfaceResumeWithCodexProvider(
					"feishu:app-1:user:ou_user",
					"app-1",
					"oc_chat",
					"ou_user",
					state.ProductModeNormal,
					agentproto.BackendCodex,
					"",
					"",
					state.SurfaceVerbosityNormal,
					state.PlanModeSettingOff,
				)
			},
			mutate: func(app *App) {
				app.service.MaterializeSurfaceResumeWithCodexProvider(
					"feishu:app-1:user:ou_other",
					"app-1",
					"oc_other",
					"ou_other",
					state.ProductModeNormal,
					agentproto.BackendCodex,
					"",
					"",
					state.SurfaceVerbosityNormal,
					state.PlanModeSettingOff,
				)
			},
			sync: func(app *App) {
				app.syncSurfaceResumeStateLocked(nil)
			},
			degrade: func(app *App, err error) {
				app.surfaceResumeRuntime.status = persistedStoreStatusDegraded
				app.surfaceResumeRuntime.diagnosticErr = err
			},
			diagnostic: func(app *App) persistedStateDiagnostic {
				runtime := app.surfaceResumeRuntime.persistedStoreRuntimeState
				return persistedStateDiagnostic{runtime.status, runtime.path, runtime.diagnosticErr, runtime.store != nil}
			},
		},
		{
			name:      "bot capability settings",
			statePath: botcapabilitysettings.StatePath,
			configure: func(app *App, stateDir string) {
				app.configureBotCapabilitySettingsStateLocked(stateDir)
			},
			seed: func(app *App) {
				app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
					GatewayID:   "app-1",
					ProductMode: state.ProductModeNormal,
					Backend:     agentproto.BackendClaude,
				}})
			},
			mutate: func(app *App) {
				app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
					GatewayID:   "app-1",
					ProductMode: state.ProductModeNormal,
					Backend:     agentproto.BackendCodex,
				}})
			},
			sync: func(app *App) {
				app.syncBotCapabilitySettingsStateLocked()
			},
			degrade: func(app *App, err error) {
				app.botCapabilitySettingsState.status = persistedStoreStatusDegraded
				app.botCapabilitySettingsState.diagnosticErr = err
			},
			diagnostic: func(app *App) persistedStateDiagnostic {
				runtime := app.botCapabilitySettingsState.persistedStoreRuntimeState
				return persistedStateDiagnostic{runtime.status, runtime.path, runtime.diagnosticErr, runtime.store != nil}
			},
		},
		{
			name:      "Feishu room",
			statePath: feishuroomstate.StatePath,
			configure: func(app *App, stateDir string) {
				app.configureFeishuRoomStateLocked(stateDir)
			},
			seed: func(app *App) {
				app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
					RoomID:           "feishu:chat:oc_room",
					ChatID:           "oc_room",
					PrimaryGatewayID: "app-1",
				}})
			},
			mutate: func(app *App) {
				app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
					RoomID:           "feishu:chat:oc_room",
					ChatID:           "oc_room",
					PrimaryGatewayID: "app-2",
				}})
			},
			sync: func(app *App) {
				app.syncFeishuRoomStateLocked()
			},
			degrade: func(app *App, err error) {
				app.feishuRoomState.status = persistedStoreStatusDegraded
				app.feishuRoomState.diagnosticErr = err
			},
			diagnostic: func(app *App) persistedStateDiagnostic {
				runtime := app.feishuRoomState.persistedStoreRuntimeState
				return persistedStateDiagnostic{runtime.status, runtime.path, runtime.diagnosticErr, runtime.store != nil}
			},
		},
		{
			name:      "Claude workspace profile",
			statePath: claudeworkspaceprofile.StatePath,
			configure: func(app *App, stateDir string) {
				app.configureClaudeWorkspaceProfileStateLocked(stateDir)
			},
			seed: func(app *App) {
				app.service.MaterializeClaudeWorkspaceProfileSnapshots(map[string]state.ClaudeWorkspaceProfileSnapshotRecord{
					"/workspace|claude|default": {
						ReasoningEffort: "high",
						AccessMode:      "full",
					},
				})
			},
			mutate: func(app *App) {
				app.service.MaterializeClaudeWorkspaceProfileSnapshots(map[string]state.ClaudeWorkspaceProfileSnapshotRecord{
					"/workspace|claude|default": {
						ReasoningEffort: "low",
						AccessMode:      "read_only",
					},
				})
			},
			sync: func(app *App) {
				app.syncClaudeWorkspaceProfileStateLocked()
			},
			degrade: func(app *App, err error) {
				app.claudeWorkspaceProfileState.status = persistedStoreStatusDegraded
				app.claudeWorkspaceProfileState.diagnosticErr = err
			},
			diagnostic: func(app *App) persistedStateDiagnostic {
				runtime := app.claudeWorkspaceProfileState.persistedStoreRuntimeState
				return persistedStateDiagnostic{runtime.status, runtime.path, runtime.diagnosticErr, runtime.store != nil}
			},
		},
	}
}

func TestPersistedStateLoadFailurePreservesOriginalAcrossSync(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name     string
		original []byte
	}{
		{name: "broken JSON", original: []byte("{broken-json\n")},
		{name: "unknown version", original: []byte("{\"version\":999,\"entries\":{}}\n")},
	}

	for _, test := range persistedStateHarnesses() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, failure := range failures {
				t.Run(failure.name, func(t *testing.T) {
					stateDir := t.TempDir()
					path := test.statePath(stateDir)
					if err := os.WriteFile(path, failure.original, 0o600); err != nil {
						t.Fatalf("write invalid state: %v", err)
					}

					app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
					app.mu.Lock()
					test.configure(app, stateDir)
					diagnostic := test.diagnostic(app)
					test.seed(app)
					test.sync(app)
					app.mu.Unlock()

					assertPersistedStateDegraded(t, diagnostic, path)
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatalf("read state after sync: %v", err)
					}
					if !bytes.Equal(got, failure.original) {
						t.Fatalf("load failure was overwritten: got %q, want original %q", got, failure.original)
					}
				})
			}
		})
	}
}

func TestPersistedStateMissingFileInitializesWritableStore(t *testing.T) {
	t.Parallel()

	for _, test := range persistedStateHarnesses() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			path := test.statePath(stateDir)
			app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			app.mu.Lock()
			test.configure(app, stateDir)
			diagnostic := test.diagnostic(app)
			test.seed(app)
			test.sync(app)
			app.mu.Unlock()

			if diagnostic.status != persistedStoreStatusWritable || diagnostic.err != nil || !diagnostic.storePresent {
				t.Fatalf("missing file did not initialize writable store: %#v", diagnostic)
			}
			if diagnostic.path != path {
				t.Fatalf("diagnostic path = %q, want %q", diagnostic.path, path)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read initialized state: %v", err)
			}
			if !json.Valid(raw) {
				t.Fatalf("initialized state is not valid JSON: %q", raw)
			}
		})
	}
}

func TestPersistedStateReloadRecoversAfterFileRepair(t *testing.T) {
	t.Parallel()

	for _, test := range persistedStateHarnesses() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			path := test.statePath(stateDir)
			if err := os.WriteFile(path, []byte("{broken-json\n"), 0o600); err != nil {
				t.Fatalf("write broken state: %v", err)
			}
			app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			app.mu.Lock()
			test.configure(app, stateDir)
			degraded := test.diagnostic(app)
			app.mu.Unlock()
			assertPersistedStateDegraded(t, degraded, path)

			if err := os.Remove(path); err != nil {
				t.Fatalf("repair state file: %v", err)
			}
			app.mu.Lock()
			test.configure(app, stateDir)
			recovered := test.diagnostic(app)
			test.seed(app)
			test.sync(app)
			app.mu.Unlock()

			if recovered.status != persistedStoreStatusWritable || recovered.err != nil || !recovered.storePresent {
				t.Fatalf("repaired state did not recover: %#v", recovered)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read recovered state: %v", err)
			}
			if !json.Valid(raw) {
				t.Fatalf("recovered state is not valid JSON: %q", raw)
			}
		})
	}
}

func TestPersistedStateIOErrorIsExplicitlyDegraded(t *testing.T) {
	t.Parallel()

	for _, test := range persistedStateHarnesses() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			path := test.statePath(stateDir)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatalf("create directory at state path: %v", err)
			}
			app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			app.mu.Lock()
			test.configure(app, stateDir)
			diagnostic := test.diagnostic(app)
			app.mu.Unlock()

			assertPersistedStateDegraded(t, diagnostic, path)
		})
	}
}

func TestPersistedStateReadOnlyDegradedBlocksSyncWrites(t *testing.T) {
	t.Parallel()

	for _, test := range persistedStateHarnesses() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stateDir := t.TempDir()
			path := test.statePath(stateDir)
			app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			app.mu.Lock()
			test.configure(app, stateDir)
			test.seed(app)
			test.sync(app)
			baseline, err := os.ReadFile(path)
			if err != nil {
				app.mu.Unlock()
				t.Fatalf("read baseline state: %v", err)
			}
			test.degrade(app, fs.ErrPermission)
			test.mutate(app)
			test.sync(app)
			app.mu.Unlock()

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read state after degraded sync: %v", err)
			}
			if !bytes.Equal(got, baseline) {
				t.Fatalf("read-only degraded store was written: got %q, want %q", got, baseline)
			}
		})
	}
}

func TestSurfaceResumeLoadFailurePreservesExistingRecoveryRuntime(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceresume.StatePath(stateDir)
	if err := os.WriteFile(path, []byte("{broken-json\n"), 0o600); err != nil {
		t.Fatalf("write broken state: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	recovery := &surfaceResumeRecoveryState{
		Entry: surfaceresume.Entry{
			SurfaceSessionID: "surface-existing",
			ResumeThreadID:   "thread-existing",
		},
		LastFailureCode: "workspace_busy",
	}
	app.surfaceResumeRuntime.recovery["surface-existing"] = recovery

	app.mu.Lock()
	app.configureSurfaceResumeStateLocked(stateDir)
	app.mu.Unlock()

	if got := app.surfaceResumeRuntime.recovery["surface-existing"]; got != recovery {
		t.Fatalf("load failure replaced existing recovery runtime: got %#v, want %#v", got, recovery)
	}
}

func TestFeishuBotIdentityLoadFailureBlocksTransitionAndPreservesOriginal(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name     string
		original []byte
	}{
		{name: "broken JSON", original: []byte("{broken-json\n")},
		{name: "unknown version", original: []byte("{\"version\":999,\"entries\":{}}\n")},
	}
	for _, failure := range failures {
		failure := failure
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			path := feishubotidentity.StatePath(stateDir)
			if err := os.WriteFile(path, failure.original, 0o600); err != nil {
				t.Fatalf("write invalid identity state: %v", err)
			}

			app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			app.mu.Lock()
			app.configureFeishuBotIdentityStateLocked(stateDir)
			runtime := app.feishuBotIdentityState.persistedStoreRuntimeState
			app.mu.Unlock()
			assertPersistedStateDegraded(t, persistedStateDiagnostic{
				status:       runtime.status,
				path:         runtime.path,
				err:          runtime.diagnosticErr,
				storePresent: runtime.store != nil,
			}, path)

			if _, err := app.planFeishuBotIdentityTransition("app-1", "cli_new"); err == nil {
				t.Fatal("identity transition was allowed while state was degraded")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read identity state after blocked transition: %v", err)
			}
			if !bytes.Equal(got, failure.original) {
				t.Fatalf("identity load failure was overwritten: got %q, want %q", got, failure.original)
			}
		})
	}
}

func TestFeishuBotIdentityReloadRecoversAfterFileRepair(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := feishubotidentity.StatePath(stateDir)
	if err := os.WriteFile(path, []byte("{broken-json\n"), 0o600); err != nil {
		t.Fatalf("write broken identity state: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuBotIdentityStateLocked(stateDir)
	app.mu.Unlock()
	if _, err := app.planFeishuBotIdentityTransition("app-1", "cli_current"); err == nil {
		t.Fatal("identity transition was allowed before repair")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("repair identity state: %v", err)
	}
	app.mu.Lock()
	app.configureFeishuBotIdentityStateLocked(stateDir)
	app.mu.Unlock()
	transition, err := app.planFeishuBotIdentityTransition("app-1", "cli_current")
	if err != nil {
		t.Fatalf("plan identity transition after repair: %v", err)
	}
	if err := app.commitFeishuBotIdentityTransition(transition); err != nil {
		t.Fatalf("commit identity transition after repair: %v", err)
	}
	identity, ok := app.feishuBotIdentityState.store.Get("app-1")
	if !ok || identity.AppID != "cli_current" || identity.Generation != 1 {
		t.Fatalf("identity after repair = %#v, present=%v", identity, ok)
	}
}

func TestFeishuBotIdentityIOErrorIsExplicitlyDegraded(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := feishubotidentity.StatePath(stateDir)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create directory at identity state path: %v", err)
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuBotIdentityStateLocked(stateDir)
	runtime := app.feishuBotIdentityState.persistedStoreRuntimeState
	app.mu.Unlock()
	assertPersistedStateDegraded(t, persistedStateDiagnostic{
		status:       runtime.status,
		path:         runtime.path,
		err:          runtime.diagnosticErr,
		storePresent: runtime.store != nil,
	}, path)
	if _, err := app.planFeishuBotIdentityTransition("app-1", "cli_current"); err == nil {
		t.Fatal("identity transition was allowed after identity state I/O error")
	}
}

func TestLoadPersistedStoreErrorsAreDegraded(t *testing.T) {
	t.Parallel()

	t.Run("load permission error", func(t *testing.T) {
		runtime := loadPersistedStore("test", "/state.json", func(string) (*fakePersistedStore, error) {
			return nil, fs.ErrPermission
		})
		if runtime.status != persistedStoreStatusDegraded || runtime.store != nil || !errors.Is(runtime.diagnosticErr, fs.ErrPermission) {
			t.Fatalf("permission error runtime = %#v", runtime)
		}
	})

	t.Run("sanitation save error", func(t *testing.T) {
		store := &fakePersistedStore{dirty: true, saveErr: fs.ErrPermission}
		runtime := loadPersistedStore("test", "/state.json", func(string) (*fakePersistedStore, error) {
			return store, nil
		})
		if runtime.status != persistedStoreStatusDegraded || runtime.store != store || !errors.Is(runtime.diagnosticErr, fs.ErrPermission) {
			t.Fatalf("sanitation error runtime = %#v", runtime)
		}
		if store.saveCalls != 1 {
			t.Fatalf("save calls = %d, want 1", store.saveCalls)
		}
	})
}

func assertPersistedStateDegraded(t *testing.T, diagnostic persistedStateDiagnostic, path string) {
	t.Helper()
	if diagnostic.status != persistedStoreStatusDegraded || diagnostic.err == nil || diagnostic.storePresent {
		t.Fatalf("load failure diagnostic = %#v, want degraded without writable store", diagnostic)
	}
	if diagnostic.path != path {
		t.Fatalf("diagnostic path = %q, want %q", diagnostic.path, path)
	}
}

type fakePersistedStore struct {
	dirty     bool
	saveErr   error
	saveCalls int
}

func (s *fakePersistedStore) Dirty() bool {
	return s.dirty
}

func (s *fakePersistedStore) Save() error {
	s.saveCalls++
	return s.saveErr
}
