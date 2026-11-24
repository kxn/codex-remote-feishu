package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// legacyPrivateHistoryAlias is a recomputable identity claim, not a grant to
// resume execution. It deliberately records the exact source carrier and the
// current verified root; the legacy literal "codex" is never a wildcard.
type legacyPrivateHistoryAlias struct {
	Version                                                        int
	ScopeID, WorkerID, Carrier, Session, Namespace, ProfileID, CWD string
	LaunchHash, EventHash, RelativePath, MetadataHash              string
}

// legacyHistoryLaunch resolves the launch from the sealed source, rather than
// trusting a mutable current worker or a caller-provided path.
func legacyHistoryLaunch(source PersonalMigrationSource, carrier workstore.CarrierRegistration) (workhost.LaunchRecord, error) {
	var result workhost.LaunchRecord
	if carrier.Owner.ScopeID != source.Source.ScopeID || carrier.Backend != work.BackendCodex {
		return result, workstore.ErrConflict
	}
	var worker work.WorkerSession
	var navigation work.PrivateNavigation
	var sourceContext work.WorkContext
	workers, navigations, contexts := 0, 0, 0
	content := map[string]workstore.PersonalExportRecord{}
	for _, record := range source.Source.Records {
		if record.Table == "content" {
			content[record.Key] = record
		}
		if record.Table == "private_navigation" {
			if json.Unmarshal(record.Payload, &navigation) != nil {
				return result, workstore.ErrConflict
			}
			navigations++
		}
		if record.Table == "contexts" && record.Key == carrier.Owner.ContextID {
			if json.Unmarshal(record.Payload, &sourceContext) != nil {
				return result, workstore.ErrConflict
			}
			contexts++
		}
		if record.Table == "workers" && record.Key == carrier.Owner.WorkerSessionID {
			if json.Unmarshal(record.Payload, &worker) != nil {
				return result, workstore.ErrConflict
			}
			workers++
		}
	}
	if navigations != 1 || contexts != 1 || navigation.PersonalScopeID != source.Source.ScopeID || navigation.FrontContextID != carrier.Owner.ContextID || navigation.RoleID != carrier.Owner.RoleID || sourceContext.ID != carrier.Owner.ContextID || sourceContext.ScopeID != source.Source.ScopeID || sourceContext.RoleID != carrier.Owner.RoleID || (sourceContext.Purpose != "" && sourceContext.Purpose != work.PurposeBusiness) {
		return result, workstore.ErrConflict
	}
	if workers != 1 || worker.ScopeID != source.Source.ScopeID || worker.NativeNamespace != "codex" || worker.Backend != work.BackendCodex || worker.ContextID != carrier.Owner.ContextID || worker.RoleID != carrier.Owner.RoleID {
		return result, workstore.ErrConflict
	}
	matches := 0
	for _, record := range source.Source.Records {
		if record.Table != "operations" {
			continue
		}
		var op work.Operation
		if json.Unmarshal(record.Payload, &op) != nil {
			return result, workstore.ErrConflict
		}
		if op.Kind != "worker.launch" || op.TargetID != carrier.Owner.WorkerSessionID || op.TargetGeneration != carrier.Owner.SessionGeneration {
			continue
		}
		item, ok := content[op.RequestRef]
		if !ok || json.Unmarshal(item.Data, &result) != nil {
			return result, workstore.ErrConflict
		}
		matches++
	}
	if matches != 1 || result.ScopeID != carrier.Owner.ScopeID || result.WorkerID != carrier.Owner.WorkerSessionID || result.Generation != carrier.Owner.SessionGeneration || result.Backend != work.BackendCodex || result.NativeHistoryNamespace != "" || result.ProfileID != source.Settings.ProfileID || result.Snapshot.Owner != carrier.Owner || result.Snapshot.CarrierIncarnation != carrier.Incarnation || !filepath.IsAbs(result.Snapshot.CWD) {
		return workhost.LaunchRecord{}, workstore.ErrConflict
	}
	return result, nil
}

// proveLegacyPrivateHistory checks the original launch and journal against the
// current profile's canonical history root. It makes no claim about the root
// or environment that the original process used, or about execution quiescence.
func (a *App) proveLegacyPrivateHistory(ctx context.Context, source PersonalMigrationSource, carrier workstore.CarrierRegistration, namespace, session string) (legacyPrivateHistoryAlias, error) {
	var alias legacyPrivateHistoryAlias
	if namespace == "" || session == "" || a == nil || a.teamRuntime == nil {
		return alias, workstore.ErrConflict
	}
	launch, err := legacyHistoryLaunch(source, carrier)
	if err != nil {
		return alias, err
	}
	if launch.Snapshot.HostID != a.teamRuntime.codexUpgradeHostID {
		return alias, workstore.ErrConflict
	}
	env, err := a.legacyCodexHistoryEnv(launch.ProfileID)
	if err != nil {
		return alias, err
	}
	currentNS, err := nativeHistoryNamespace(agentproto.BackendCodex, env, launch.Snapshot.CWD, launch.Snapshot.HostID)
	if err != nil || currentNS != namespace {
		return alias, workstore.ErrConflict
	}
	launchHash, eventHash, err := runtimejournal.VerifyLegacySessionProvenance(ctx, launch.Snapshot, a.headlessRuntime.Paths.StateDir, session)
	if err != nil {
		return alias, err
	}
	root, err := nativeHistoryRoot(agentproto.BackendCodex, env, launch.Snapshot.CWD)
	if err != nil {
		return alias, err
	}
	path, meta, err := findLegacyCodexSession(ctx, filepath.Join(root, "sessions"), session, launch.Snapshot.CWD)
	if err != nil {
		return alias, err
	}
	if path == "" || meta == "" || launchHash == "" || eventHash == "" {
		return alias, fmt.Errorf("legacy history evidence incomplete")
	}
	return legacyPrivateHistoryAlias{Version: 1, ScopeID: source.Source.ScopeID, WorkerID: carrier.Owner.WorkerSessionID, Carrier: carrier.Incarnation, Session: session, Namespace: namespace, ProfileID: launch.ProfileID, CWD: launch.Snapshot.CWD, LaunchHash: launchHash, EventHash: eventHash, RelativePath: path, MetadataHash: meta}, nil
}

// legacyHistorySource resolves the committed transfer from the authenticated
// private owner and rechecks the sealed source against the target receipt.
func (g *applicationIngress) legacyHistorySource(ctx context.Context, owner privatechat.Owner, generation int64) (PersonalMigrationSource, workstore.PersonalMigrationSeal, error) {
	var source PersonalMigrationSource
	var seal workstore.PersonalMigrationSeal
	if g == nil || g.host == nil || g.host.applications == nil || g.host.applications.migrations == nil {
		return source, seal, workstore.ErrConflict
	}
	c := g.host.applications.migrations
	binding, err := g.privatePublicationBindingFor(ctx, owner, generation)
	if err != nil {
		return source, seal, err
	}
	botGeneration, err := g.generation(ctx, owner.GatewayID, owner.AppID)
	if err != nil {
		return source, seal, err
	}
	original, err := g.router.LookupImportedOwner(ctx, owner.TenantID, owner.AppID, owner.GatewayID, botGeneration, owner.ChatID, owner.UserID)
	if err != nil {
		return source, seal, err
	}
	migrationID, target, err := g.router.CommittedMigration(ctx, original)
	if err != nil || target != binding.Owner {
		return source, seal, workstore.ErrConflict
	}
	scope, err := g.router.OwnerScopeBindingFor(ctx, original)
	if err != nil {
		return source, seal, err
	}
	expectedID, importID, err := c.MigrationIDs(ctx, scope)
	if err != nil || expectedID != migrationID {
		return source, seal, workstore.ErrConflict
	}
	receipt, err := c.journal.MigrationReceipt(ctx, owner, generation, importID)
	if err != nil {
		return source, seal, err
	}
	if err = c.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(scope.ScopeID, migrationID)
		return e
	}); err != nil {
		return source, seal, err
	}
	if !legacyHistoryReceiptMatches(receipt, seal, scope.ScopeID) || json.Unmarshal(seal.Manifest, &source) != nil || source.Verify() != nil || source.Source.ScopeID != scope.ScopeID {
		return PersonalMigrationSource{}, workstore.PersonalMigrationSeal{}, workstore.ErrConflict
	}
	return source, seal, nil
}

// legacyHistoryAllowed evaluates every known historical carrier for this exact
// session. Another scope with an unresolved or equal namespace always wins
// over an alias, so a matching source never erases a team ownership conflict.
func (g *applicationIngress) legacyHistoryAllowed(ctx context.Context, owner privatechat.Owner, generation int64, backend agentproto.Backend, namespace, session string, resume bool) (bool, error) {
	if backend != agentproto.BackendCodex || namespace == "" || session == "" {
		return false, nil
	}
	source, seal, err := g.legacyHistorySource(ctx, owner, generation)
	if err != nil {
		return false, err
	}
	profile, err := g.effectiveLegacyHistoryProfile(ctx, owner, generation)
	if err != nil || profile != state.NormalizeCodexProfileID(source.Settings.ProfileID) {
		return false, nil
	}
	var carriers []workstore.CarrierRegistration
	launches := map[string]workhost.LaunchRecord{}
	err = g.host.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		carriers, e = tx.HistoricalSessionCarriers(session)
		if e != nil {
			return e
		}
		for _, carrier := range carriers {
			if carrier.Backend != work.BackendCodex || carrier.Owner.ScopeID == source.Source.ScopeID {
				continue
			}
			op, e := tx.GetOperation(workhost.LaunchOperationID(carrier.Owner.WorkerSessionID, carrier.Owner.SessionGeneration))
			if e != nil {
				return workstore.ErrConflict
			}
			body, e := tx.GetContent(carrier.Owner.ScopeID, op.RequestRef)
			if e != nil {
				return e
			}
			var launch workhost.LaunchRecord
			if json.Unmarshal(body.Data, &launch) != nil || launch.ScopeID != carrier.Owner.ScopeID || launch.WorkerID != carrier.Owner.WorkerSessionID || launch.Generation != carrier.Owner.SessionGeneration || launch.Snapshot.CarrierIncarnation != carrier.Incarnation || launch.Backend != carrier.Backend {
				return workstore.ErrConflict
			}
			launches[carrier.Incarnation] = launch
		}
		return nil
	})
	if errors.Is(err, workstore.ErrConflict) || errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrScopeDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	found := false
	for _, carrier := range carriers {
		if carrier.Backend != work.BackendCodex {
			continue
		}
		if carrier.Owner.ScopeID != source.Source.ScopeID {
			ns := launches[carrier.Incarnation].NativeHistoryNamespace
			if ns == "" || ns == namespace {
				return false, nil
			}
			continue
		}
		if _, err = g.host.proveLegacyPrivateHistory(ctx, source, carrier, namespace, session); err != nil {
			return false, nil
		}
		if resume {
			if !legacyCarrierQuiesced(seal.Evidence, carrier) {
				return false, nil
			}
			var proof personalSourceSealEvidence
			if json.Unmarshal(seal.Evidence, &proof) != nil {
				return false, workstore.ErrConflict
			}
			for _, worker := range proof.Workers {
				if worker.WorkerID == carrier.Owner.WorkerSessionID && worker.Isolated != nil {
					if err = g.host.teamRuntime.verifyPersonalExecutionIsolation(ctx, *worker.Isolated); err != nil {
						return false, nil
					}
				}
			}
		}

		found = true
	}
	return found, nil
}

// A worker's newer generation cannot prove an older carrier quiescent.
func legacyCarrierQuiesced(raw []byte, carrier workstore.CarrierRegistration) bool {
	var evidence personalSourceSealEvidence
	if json.Unmarshal(raw, &evidence) != nil {
		return false
	}
	matches := 0
	allowed := false
	for _, worker := range evidence.Workers {
		if worker.WorkerID != carrier.Owner.WorkerSessionID {
			continue
		}
		matches++
		if worker.Isolated == nil {
			continue
		}
		q := worker.Isolated
		allowed = q.Worker.ID == carrier.Owner.WorkerSessionID && q.Worker.ScopeID == carrier.Owner.ScopeID && q.Worker.ContextID == carrier.Owner.ContextID && q.Worker.Generation == carrier.Owner.SessionGeneration && q.Worker.ControllerGeneration == carrier.Owner.ControllerGeneration && q.Worker.CarrierIncarnation == carrier.Incarnation && q.ProcessScopesStopped && q.NativeCommandsIdle && q.InputsIdle && q.ToolsIdle && q.GoalsIdle && q.TasksIdle && q.TurnsTerminal && q.UncertaintiesEmpty
	}
	return matches == 1 && allowed
}

func legacyHistoryReceiptMatches(receipt privatechat.MigrationImportReceipt, seal workstore.PersonalMigrationSeal, scope string) bool {
	return scope != "" && seal.ScopeID == scope && receipt.SourceScope == scope && receipt.SourceHash == seal.SourceHash && receipt.ProofHash == migrationBytesHash(seal.Evidence) && migrationBytesHash(seal.Manifest) == seal.SourceHash
}
