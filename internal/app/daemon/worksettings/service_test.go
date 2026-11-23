package worksettings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type settingsFakePort struct {
	store     *workstore.Store
	snapshot  Snapshot
	calls     int
	failAfter bool
	binding   work.RoleBotBinding
}

func (p *settingsFakePort) Snapshot(_ context.Context, b work.RoleBotBinding) (Snapshot, error) {
	if b != p.binding {
		return Snapshot{}, ErrConflict
	}
	return p.snapshot, nil
}
func (p *settingsFakePort) ApplyBot(ctx context.Context, b work.RoleBotBinding, expected string, next state.BotCapabilitySettingsRecord, evidence Evidence) (Applied, error) {
	p.calls++
	if b != p.binding || expected != p.snapshot.Fingerprint {
		return Applied{}, ErrConflict
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := p.store.Within(bounded, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(evidence.OperationID)
		if err != nil {
			return err
		}
		if op.State != work.OperationSending || op.RequestRef == "" {
			return errors.New("effect not durably accepted before write")
		}
		return nil
	}); err != nil {
		return Applied{}, err
	}
	p.snapshot.Settings = next
	p.snapshot.Settings.UpdatedBy = evidence.PrincipalID
	p.snapshot.Settings.UpdatedAt = time.Now().UTC()
	p.snapshot.Fingerprint = "fingerprint-2"
	applied := Applied{Fingerprint: p.snapshot.Fingerprint, DesiredRevision: 2, Evidence: "Fake actual settings owner receipt"}
	if p.failAfter {
		return applied, errors.New("receipt lost after durable settings write")
	}
	return applied, nil
}
func settingsFixture(t *testing.T) (*Service, *settingsFakePort, workstore.PrivateActionSource) {
	t.Helper()
	store, err := workstore.Open(context.Background(), filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	now := time.Date(2026, 11, 21, 19, 0, 0, 0, time.UTC)
	binding := work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "Work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(binding, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: "human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "personal", RoleID: "role", Workstream: "main", Revision: 1}, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "personal", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		body := work.NewContent("human-body", "personal", "text/plain", []byte("Use the selected model and settings for this assistant."), now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		message := work.Message{ID: "human-message", ScopeID: "personal", ConversationID: "private", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "private", MessageID: "platform-message"}, BodyRef: body.ID, Revision: 1}
		inbox := work.Inbox{ID: "input", ScopeID: "personal", RecipientRoleID: "role", RecipientContextID: "context", EventID: "human-message", State: work.InboxPending, Revision: 1}
		if _, _, err := tx.AcceptMessage(message, []work.Inbox{inbox}); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "attempt", ScopeID: "personal", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "start", Revision: 1}
		op := work.Operation{ID: "start", ScopeID: "personal", ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestHash: "start", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, StartOperation: op, Now: now}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, ProductMode: state.ProductModeNormal, CodexProfileID: state.NativeCodexProfileID})
	fake := &settingsFakePort{store: store, binding: binding, snapshot: Snapshot{Settings: record, Fingerprint: "fingerprint-1", Profiles: []Profile{{ID: state.NativeCodexProfileID, Name: "Native", Backend: agentproto.BackendCodex, Available: true}, {ID: "fixed", Name: "Fixed", Backend: agentproto.BackendCodex, Available: true, FixedModel: "configured-model", FixedReasoning: "high"}}, Catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "model", Model: "real-model", IsDefault: true, SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}}}, CatalogBackend: agentproto.BackendCodex, CatalogProfileID: state.NativeCodexProfileID}}
	service, err := New(store, fake, func(_ context.Context, p string, b work.RoleBotBinding, r work.SourceRef) error {
		if p != "human" || b != binding || r.ID != "human-message" {
			return workstore.ErrScopeDenied
		}
		return nil
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return service, fake, workstore.PrivateActionSource{PersonalScopeID: "personal", MessageID: "human-message", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}}
}
func TestBotSettingsActualSourceCASAndUnknownReconciliation(t *testing.T) {
	service, port, source := settingsFixture(t)
	model := "real-model"
	req := UpdateRequest{Key: "select", RoleID: "role", ExpectedFingerprint: "fingerprint-1", Model: &model}
	if _, err := service.UpdateBot(context.Background(), "impostor", source, req); err == nil {
		t.Fatal("model supplied a fake principal")
	}
	if port.calls != 0 {
		t.Fatal("invalid authority reached settings owner")
	}
	port.failAfter = true
	first, err := service.UpdateBot(context.Background(), "human", source, req)
	if err == nil || first.Status != "pending_external" || port.calls != 1 {
		t.Fatalf("uncertain write %#v %v", first, err)
	}
	replay, err := service.UpdateBot(context.Background(), "human", source, req)
	if err != nil || !replay.Replay || port.calls != 1 {
		t.Fatalf("unknown write repeated %#v %v", replay, err)
	}
	reconciled, err := service.Reconcile(context.Background(), first.OperationID)
	if err != nil || reconciled.Status != "completed" || port.calls != 1 {
		t.Fatalf("read-only reconcile %#v %v", reconciled, err)
	}
	req.Key = "old-fingerprint"
	if _, err = service.UpdateBot(context.Background(), "human", source, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("CAS bypassed %v", err)
	}
}
func TestSettingsNoCatalogDoesNotInventDynamicModelAndFixedProfileRemainsSelectable(t *testing.T) {
	service, port, source := settingsFixture(t)
	port.snapshot.Catalog = nil
	model := "arbitrary-guessed-model"
	if _, err := service.UpdateBot(context.Background(), "human", source, UpdateRequest{Key: "guess", RoleID: "role", ExpectedFingerprint: "fingerprint-1", Model: &model}); err == nil {
		t.Fatal("missing catalog became positive model validation")
	}
	profile := "fixed"
	result, err := service.UpdateBot(context.Background(), "human", source, UpdateRequest{Key: "configured-profile", RoleID: "role", ExpectedFingerprint: "fingerprint-1", ProfileID: &profile})
	if err != nil || result.Status != "completed" || port.calls != 1 {
		t.Fatalf("explicit configured profile %#v %v", result, err)
	}
	if port.snapshot.Settings.PromptOverride.Model != "" {
		t.Fatal("fixed profile duplicated a model override instead of preserving its own source")
	}
}
func TestContextPreferencesArePrivateScopedVersionedAndSeparateFromBot(t *testing.T) {
	service, port, source := settingsFixture(t)
	access := "confirm"
	plan := "on"
	verbose := "chatty"
	req := ContextUpdateRequest{Key: "context-choice", ContextID: "context", AccessMode: &access, PlanMode: &plan, Verbose: &verbose}
	first, err := service.UpdateContext(context.Background(), "human", source, req)
	if err != nil || first.Revision != 1 || *first.Verbose != "chatty" {
		t.Fatalf("context prefs %#v %v", first, err)
	}
	if port.calls != 0 || port.snapshot.Settings.PromptOverride.AccessMode != "" {
		t.Fatal("context preferences changed global Bot configuration")
	}
	replay, err := service.UpdateContext(context.Background(), "human", source, req)
	if err != nil || replay.Revision != 1 {
		t.Fatalf("local replay %#v %v", replay, err)
	}
	req.Key = "stale"
	if _, err = service.UpdateContext(context.Background(), "human", source, req); !errors.Is(err, workstore.ErrConflict) {
		t.Fatalf("context preference CAS bypassed: %v", err)
	}
	req.Key = "foreign"
	req.ContextID = "another-private-context"
	if _, err = service.UpdateContext(context.Background(), "human", source, req); err == nil {
		t.Fatal("foreign context silently selected")
	}
}

type scopedCatalogPort struct {
	*settingsFakePort
	owners []CatalogOwner
}

func (p *scopedCatalogPort) ReadCatalog(_ context.Context, binding work.RoleBotBinding, owner CatalogOwner) (ObservedCatalog, error) {
	p.owners = append(p.owners, owner)
	snapshot := p.snapshot.Catalog
	if owner.Cursor == "page-2" {
		snapshot = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "second-model", Model: "second-model"}}}
	}
	return ObservedCatalog{Snapshot: snapshot, Evidence: "actual-native-model-command"}, nil
}
func TestSettingsCatalogUsesVerifiedConfigurationContextAndPage(t *testing.T) {
	s, fake, source := settingsFixture(t)
	port := &scopedCatalogPort{settingsFakePort: fake}
	s.port = port
	snapshot, err := s.DiscoverPage(t.Context(), "human", "role", source, "page-2")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Catalog == nil || snapshot.Catalog.Entries[0].Model != "second-model" || len(port.owners) != 1 {
		t.Fatalf("catalog %+v", snapshot)
	}
	owner := port.owners[0]
	if owner.Cursor != "page-2" || owner.ContextID != "context" || owner.SettingsRevision != 1 || owner.Backend != agentproto.BackendCodex || owner.ProfileID != state.NativeCodexProfileID {
		t.Fatalf("wrong frozen catalog configuration %+v", owner)
	}
}
