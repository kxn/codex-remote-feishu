package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type teamSettingsTestRuntime struct{}

func (teamSettingsTestRuntime) SendCommand(context.Context, string, agentproto.Command) error {
	return nil
}
func (teamSettingsTestRuntime) Inspect(_ context.Context, req workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	return workruntime.NativeObservation{
		CarrierIncarnation: req.Carrier.Incarnation, InstanceID: req.Carrier.OriginInstanceID,
		InstanceGeneration: req.Carrier.InstanceGeneration, ProcessGeneration: req.Carrier.ProcessGeneration,
		LocationGeneration: req.LocationGeneration, SessionID: req.SessionID, TurnID: req.TurnID,
		NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, ObservedAt: time.Now(),
		NativeEvidence: []byte(`{"native":"stopped"}`), ContinuationEvidence: []byte(`{"goal":"stopped"}`),
		BackgroundEvidence: []byte(`{"background":"stopped"}`),
	}, nil
}

func setTestWorkBotSettings(t *testing.T, app *App, record state.BotCapabilitySettingsRecord) {
	t.Helper()
	record, ok := state.NormalizeBotCapabilitySettingsRecord(record)
	if !ok {
		t.Fatal("invalid Bot fixture settings")
	}
	if err := app.botCapabilitySettingsState.store.Put(record); err != nil {
		t.Fatal(err)
	}
	app.materializeBotCapabilitySettingsStateLocked()
}

func TestTeamClaudeSettingChangesRebuildOnlyAfterWorkerIsIdle(t *testing.T) {
	ctx := t.Context()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	inheritedAuth := config.ClaudeAuthTokenEnv + "=existing-native-login"
	app.headlessRuntime.BaseEnv = []string{inheritedAuth}
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude})
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app.teamRuntime = &teamRuntime{store: store}
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", "gateway")
	scope := work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: roleID, PrincipalID: "human", Revision: 1}
	pickerTx(t, app, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutScope(scope, 0)
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	var launches []relayruntime.HeadlessLaunchOptions
	stops := 0
	host, err := workhost.New(store, service, signer, workhost.Config{
		Paths: app.headlessRuntime.Paths, BinaryPath: "test-wrapper", HostID: "host", ToolEndpoint: "http://127.0.0.1:9090",
		Settings: teamSettingsResolver{app: app, store: store, hostID: "host"},
		Registry: workruntime.New(store, workruntime.Options{}), Runtime: teamSettingsTestRuntime{},
		Start: func(options relayruntime.HeadlessLaunchOptions) (int, error) {
			launches = append(launches, options)
			return 1234, nil
		},
		StopCarrier: func(context.Context, work.WorkerSession) error { stops++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	need := workengine.WorkerNeed{ScopeID: scope.ID, RoleID: roleID}
	if err = host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	var candidate workengine.Candidate
	loadCandidate := func(tx *workstore.Tx) error {
		candidate.Scope = scope
		var err error
		candidate.Role, err = tx.GetRole(roleID)
		if err != nil {
			return err
		}
		candidate.BotBinding, err = tx.GetRoleBotBinding(roleID)
		if err != nil {
			return err
		}
		workers, err := tx.ListWorkers(scope.ID)
		if err != nil {
			return err
		}
		if len(workers) != 1 {
			t.Fatalf("workers = %d", len(workers))
		}
		candidate.Worker = workers[0]
		candidate.Context, err = tx.GetContext(candidate.Worker.ContextID)
		return err
	}
	setWorkerState := func(tx *workstore.Tx, next work.WorkerState) error {
		if err := loadCandidate(tx); err != nil {
			return err
		}
		worker := candidate.Worker
		worker.State = next
		worker.Revision++
		return tx.PutWorker(worker, worker.Revision-1)
	}
	pickerTx(t, app, func(tx *workstore.Tx) error { return setWorkerState(tx, work.WorkerReady) })
	for i := 0; i < 5; i++ {
		if err = host.Prepare(ctx, need); err != nil {
			t.Fatal(err)
		}
	}
	if len(launches) != 1 || stops != 0 {
		t.Fatalf("unchanged effective settings rebuilt carrier: launches=%d stops=%d", len(launches), stops)
	}
	pickerTx(t, app, func(tx *workstore.Tx) error { return setWorkerState(tx, work.WorkerBusy) })
	before := candidate
	port := teamBotSettingsPort{app: app}
	oldSettings, err := port.Snapshot(ctx, before.BotBinding)
	if err != nil {
		t.Fatal(err)
	}
	preference, _ := app.profileContextPreferenceState.store.ClaudeCurrent(config.ClaudeDefaultProfileID)
	updated := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/claude/profiles/default/context-preference", `{"mode":"extended_1m"}`, preference.ETag)
	if updated.Code != http.StatusOK {
		t.Fatalf("profile update = %d, %s", updated.Code, updated.Body.String())
	}
	newSettings, err := port.Snapshot(ctx, before.BotBinding)
	if err != nil || oldSettings.Fingerprint == newSettings.Fingerprint {
		t.Fatalf("snapshot did not reflect the committed Claude context preference: %v", err)
	}
	// Admin publishes to its config store after planning; admission refreshes the
	// actual desired revision before the periodic projection can run.
	if err = app.workSettingsAdmission(store).Within(ctx, before.Role.ID, func() error {
		return store.Within(ctx, func(tx *workstore.Tx) error {
			role, e := tx.GetRole(before.Role.ID)
			if e != nil {
				return e
			}
			if role.Revision != before.Role.Revision {
				return workstore.ErrConflict
			}
			return nil
		})
	}); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("admission accepted the old role version: %v", err)
	}
	pickerTx(t, app, loadCandidate)
	if candidate.Role.SettingsRevision != before.Role.SettingsRevision+1 || candidate.Worker.AppliedSettingsRevision != before.Worker.AppliedSettingsRevision {
		t.Fatal("desired and applied settings did not retain separate revisions")
	}
	if _, err := host.Plan(ctx, candidate); !errors.Is(err, workengine.ErrPreparationRequired) {
		t.Fatalf("old worker could plan new work: %v", err)
	}
	if err := host.Prepare(ctx, need); err == nil || len(launches) != 1 || stops != 0 {
		t.Fatalf("busy worker was replaced: launches=%d stops=%d err=%v", len(launches), stops, err)
	}
	pickerTx(t, app, func(tx *workstore.Tx) error { return setWorkerState(tx, work.WorkerReady) })
	if err = host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, app, loadCandidate)
	extendedModel := config.ClaudeModelEnv + "=sonnet[1m]"
	if len(launches) != 2 || stops != 1 || slices.Contains(launches[0].Env, extendedModel) || !slices.Contains(launches[1].Env, extendedModel) {
		t.Fatalf("Claude replacement count or launch setting mismatch, launches=%d stops=%d", len(launches), stops)
	}
	if candidate.Worker.Generation != before.Worker.Generation+1 || candidate.Worker.AppliedSettingsRevision != candidate.Role.SettingsRevision {
		t.Fatal("new worker did not freeze new effective settings")
	}
	pickerTx(t, app, func(tx *workstore.Tx) error { return setWorkerState(tx, work.WorkerReady) })
	selected := newSettings.Settings
	selected.PromptOverride.Model = "opus"
	selected.PromptOverride.ReasoningEffort = "high"
	evidence := worksettings.Evidence{PrincipalID: scope.PrincipalID, OperationID: "selection", Source: work.SourceRef{Kind: "test", ID: "selection", Version: "1", EvidenceRef: "selection"}}
	applied, err := port.ApplyBot(ctx, candidate.BotBinding, newSettings.Fingerprint, selected, evidence)
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, app, loadCandidate)
	pickerTx(t, app, func(tx *workstore.Tx) error {
		return tx.CheckRoleSettingsReference(candidate.Role.ID, applied.Fingerprint, applied.DesiredRevision)
	})
	if _, err := host.Plan(ctx, candidate); !errors.Is(err, workengine.ErrPreparationRequired) {
		t.Fatalf("warm worker accepted newly selected model and effort without preparation: %v", err)
	}
	if err = host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	if len(launches) != 3 || stops != 2 {
		t.Fatalf("model and effort change did not rebuild the idle carrier: launches=%d stops=%d", len(launches), stops)
	}
	nativeEnv := launches[2].Env
	if !slices.Contains(nativeEnv, config.ClaudeModelEnv+"=opus[1m]") ||
		!slices.Contains(nativeEnv, config.ClaudeEffortLevelEnv+"=high") ||
		!slices.Contains(nativeEnv, inheritedAuth) ||
		slices.Contains(launches[1].Env, config.ClaudeEffortLevelEnv+"=high") {
		t.Fatal("new Claude carrier did not receive explicit model, effort, and inherited authentication")
	}

	loaded, err := app.loadAdminConfig()
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	loaded.Config.Feishu.Apps[0].Enabled = &disabled
	if err := config.WriteAppConfig(loaded.Path, loaded.Config); err != nil {
		t.Fatal(err)
	}
	if err := app.workSettingsAdmission(store).Within(ctx, candidate.Role.ID, func() error {
		t.Fatal("disabled Bot reached the launch or claim transaction")
		return nil
	}); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("disabled Bot remained eligible: %v", err)
	}

}

func TestTeamOpenCodeLaunchUsesContextAccessAndCurrentProfile(t *testing.T) {
	ctx := t.Context()
	input := config.OpenCodeAPIProfileInput{Name: "Team", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "old-model"}
	profile, err := config.PrepareOpenCodeAPIProfileCreate(nil, input)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "secret"}}
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{profile}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	seed, source := teamPickerFixture(t)
	app.teamRuntime = seed.teamRuntime
	record := state.BotCapabilitySettingsRecord{
		GatewayID: "gateway", Backend: agentproto.BackendOpenCode, OpenCodeProfileID: profile.ID,
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: profile.ID, Revision: 1}},
		PromptOverride:       state.ModelConfigRecord{AccessMode: agentproto.AccessModeFullAccess},
		PlanMode:             state.PlanModeSettingOn, PlanModeOverrideSet: true,
	}
	setTestWorkBotSettings(t, app, record)
	// Publish a current revision while retaining the migrated Bot's legacy admission.
	input.Model = "new-model"
	profile, changed, err := config.PrepareOpenCodeAPIProfileUpdate(profile, input)
	if err != nil || !changed {
		t.Fatalf("profile revision: changed=%t err=%v", changed, err)
	}
	cfg.OpenCode.Profiles[0] = profile
	if err = config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.loadAdminConfig()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := app.resolveWorkBotSettings(app.workBotSettingsLocked(source.Gateway.GatewayRef), loaded.Config).fingerprint
	var scope work.WorkScope
	var wc work.WorkContext
	var role work.Role
	pickerTx(t, app, func(tx *workstore.Tx) error {
		if err := tx.SyncRoleSettingsReference(source.RoleID, fingerprint); err != nil {
			return err
		}
		var err error
		scope, err = tx.GetScope(source.ScopeID)
		if err != nil {
			return err
		}
		role, err = tx.GetRole(source.RoleID)
		if err != nil {
			return err
		}
		wc, _, err = worksettings.FrozenContext(tx, source.Authority)
		return err
	})
	resolver := teamSettingsResolver{app: app, store: app.teamRuntime.store, hostID: "host"}
	assertAccess := func(want string, wantAsk bool, wantRevision int64) {
		t.Helper()
		launch, err := resolver.ResolveLaunch(ctx, scope, role, wc)
		if err != nil {
			t.Fatal(err)
		}
		var material string
		for _, entry := range launch.Environment {
			if value, ok := strings.CutPrefix(entry, config.OpenCodeConfigContentEnv+"="); ok {
				material = value
			}
		}
		if !strings.Contains(material, "new-model") || strings.Contains(material, "old-model") ||
			strings.Contains(material, `"*":"ask"`) != wantAsk || strings.Contains(material, `"*":"allow"`) != (want == agentproto.AccessModeFullAccess) ||
			launch.Overrides.AccessMode != want || launch.Overrides.PlanMode != "on" || launch.PreferencesRevision != wantRevision {
			t.Fatalf("effective launch did not match the authoritative profile and WorkContext access/plan: overrides=%+v revision=%d", launch.Overrides, launch.PreferencesRevision)
		}
	}
	assertAccess(agentproto.AccessModeFullAccess, false, 0)
	settings, err := app.teamPreferencesService()
	if err != nil {
		t.Fatal(err)
	}
	confirm := agentproto.AccessModeConfirm
	if _, err = settings.UpdateContext(ctx, source.PrincipalID, source.Authority, worksettings.ContextUpdateRequest{Key: "confirm", ContextID: wc.ID, AccessMode: &confirm}); err != nil {
		t.Fatal(err)
	}
	assertAccess(confirm, true, 1)
	if _, err = settings.UpdateContext(ctx, source.PrincipalID, source.Authority, worksettings.ContextUpdateRequest{Key: "reset", ContextID: wc.ID, ExpectedRevision: 1, Reset: []string{"access"}}); err != nil {
		t.Fatal(err)
	}
	assertAccess(agentproto.AccessModeFullAccess, false, 2)
}

func TestAdminCannotDeleteSelectedTeamClaudeProfile(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	create := performAdminRequest(t, app, http.MethodPost, "/api/admin/claude/profiles", `{"name":"Dev","model":"claude-sonnet-4-5"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("profile create = %d, %s", create.Code, create.Body.String())
	}
	app.teamRuntime = &teamRuntime{}
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: "dev"})
	del := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/dev", "")
	if del.Code != http.StatusConflict {
		t.Fatalf("selected profile delete = %d, %s", del.Code, del.Body.String())
	}
	assertAdminAPIErrorCode(t, del, "profile_in_use")
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: config.ClaudeDefaultProfileID})
	del = performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/dev", "")
	if del.Code != http.StatusNoContent {
		t.Fatalf("unselected profile delete = %d, %s", del.Code, del.Body.String())
	}
}

func TestTeamCodexNativeContextPreferenceRebuildsWarmWorker(t *testing.T) {
	ctx := t.Context()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app.teamRuntime = &teamRuntime{store: store}
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", "gateway")
	scope := work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: roleID, PrincipalID: "human", Revision: 1}
	pickerTx(t, app, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutScope(scope, 0)
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	var launches []relayruntime.HeadlessLaunchOptions
	stops := 0
	host, err := workhost.New(store, service, signer, workhost.Config{
		Paths: app.headlessRuntime.Paths, BinaryPath: "test-wrapper", HostID: "host", ToolEndpoint: "http://127.0.0.1:9090",
		Settings: teamSettingsResolver{app: app, store: store, hostID: "host"},
		Registry: workruntime.New(store, workruntime.Options{}), Runtime: teamSettingsTestRuntime{},
		Start: func(options relayruntime.HeadlessLaunchOptions) (int, error) {
			launches = append(launches, options)
			return 1234, nil
		},
		StopCarrier: func(context.Context, work.WorkerSession) error { stops++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	need := workengine.WorkerNeed{ScopeID: scope.ID, RoleID: roleID}
	if err = host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, app, func(tx *workstore.Tx) error {
		workers, err := tx.ListWorkers(scope.ID)
		if err != nil || len(workers) != 1 {
			t.Fatalf("workers=%d err=%v", len(workers), err)
		}
		worker := workers[0]
		worker.State = work.WorkerReady
		worker.Revision++
		return tx.PutWorker(worker, worker.Revision-1)
	})
	preference, _ := app.profileContextPreferenceState.store.CodexCurrent(config.CodexNativeProfileID)
	updated := performAdminRequestWithIfMatch(t, app, http.MethodPut, "/api/admin/codex/profiles/"+config.CodexNativeProfileID+"/context-preference", `{"mode":"price_guard_272k"}`, preference.ETag)
	if updated.Code != http.StatusOK {
		t.Fatalf("profile update = %d, %s", updated.Code, updated.Body.String())
	}
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	if err = host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	if len(launches) != 2 || stops != 1 {
		t.Fatalf("new preference did not replace the warm worker: launches=%d stops=%d", len(launches), stops)
	}
	pickerTx(t, app, func(tx *workstore.Tx) error {
		role, err := tx.GetRole(roleID)
		if err != nil {
			return err
		}
		workers, err := tx.ListWorkers(scope.ID)
		if err != nil {
			return err
		}
		if role.SettingsRevision != 2 || len(workers) != 1 || workers[0].AppliedSettingsRevision != role.SettingsRevision {
			t.Fatalf("Codex preference was not frozen as part of the role revision: role=%+v workers=%+v", role, workers)
		}
		launches, err := tx.ListOperations(scope.ID)
		if err != nil {
			return err
		}
		var old, fresh *agentproto.CodexResumePolicy
		for _, launch := range launches {
			if launch.Kind != "worker.launch" {
				continue
			}
			body, err := tx.GetContent(scope.ID, launch.RequestRef)
			if err != nil {
				return err
			}
			var record workhost.LaunchRecord
			if err := json.Unmarshal(body.Data, &record); err != nil {
				return err
			}
			if record.Generation == 1 {
				old = record.CodexResume
			} else {
				fresh = record.CodexResume
			}
		}
		if old == nil || !old.InheritNativeProvider || old.ContextWindow != 0 ||
			fresh == nil || !fresh.InheritNativeProvider || fresh.ModelProviderID != "" ||
			fresh.ContextWindow != 272000 || fresh.AutoCompactLimit != 244800 {
			t.Fatalf("native Codex frozen context policy did not change with preference: old=%+v fresh=%+v", old, fresh)
		}
		return nil
	})
}
