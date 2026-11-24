package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// teamSealedImportVerifier reads source and neutral proof itself. It never
// treats a caller-provided manifest or snapshot hash as authority.
type teamSealedImportVerifier struct {
	app       *App
	router    *ingressowner.Router
	binding   ingressowner.OwnerScopeBinding
	target    ingressowner.Owner
	admitter  privateApplicationAdmitter
	assetRoot string
	limits    workstore.PersonalExportLimits
}

func migrationBytesHash(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (v teamSealedImportVerifier) LoadVerifiedMigration(ctx context.Context, req privatechat.MigrationImportRequest) (privatechat.MigrationImportBundle, error) {
	var out privatechat.MigrationImportBundle
	if v.app == nil || v.app.teamRuntime == nil || v.router == nil || req.SourceHandle == "" || v.binding.ScopeID == "" || v.target.Application != ingressowner.Private {
		return out, privatechat.ErrJournalState
	}
	targetBinding, err := v.router.PrivateBindingForOwner(ctx, v.binding.Tenant, v.binding.AppID, v.binding.GatewayID, v.binding.BotGeneration, v.binding.ChatID, v.target)
	if err != nil {
		return out, err
	}
	expected := privatechat.Owner{GatewayID: targetBinding.GatewayID, SurfaceID: gatewaypkg.SurfaceIDForInbound(targetBinding.GatewayID, "", "p2p", targetBinding.Operator.ID), ChatID: targetBinding.ChatID, UserID: targetBinding.Operator.ID, TenantID: targetBinding.Tenant, AppID: targetBinding.AppID}
	if req.Owner != expected || req.Generation != v.target.Epoch {
		return out, privatechat.ErrJournalConflict
	}
	var seal workstore.PersonalMigrationSeal
	if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(v.binding.ScopeID, req.SourceHandle)
		return e
	}); err != nil {
		return out, err
	}
	if err := v.app.verifyPersonalSourceSeal(ctx, v.router, v.binding, seal, v.limits); err != nil {
		return out, err
	}
	historicalVerifier := teamHistoricalMigrationVerifier{app: v.app, router: v.router, assetRoot: v.assetRoot, limits: v.limits}
	v.router.SetHistoricalSourceVerifier(historicalVerifier)
	v.router.SetHistoricalStagingVerifier(historicalVerifier)
	var source PersonalMigrationSource
	if err := json.Unmarshal(seal.Manifest, &source); err != nil {
		return out, err
	}
	if err := source.Verify(); err != nil {
		return out, err
	}
	report, assets, err := mapPersonalMigrationSourceWithAssets(source, req.Owner, v.assetRoot)
	if err != nil {
		return out, err
	}
	for _, blocker := range report.Blockers {
		if blocker.Reason == "missing_or_ambiguous_navigation" || blocker.Reason == "project_view_requires_separate_restore" {
			return out, fmt.Errorf("unmapped source %s/%s: %s", blocker.Table, blocker.Key, blocker.Reason)
		}
	}
	out.SourceScope = seal.ScopeID
	out.SourceHash = seal.SourceHash
	out.ProofHash = migrationBytesHash(seal.Evidence)
	out.Checkpoint = report.Checkpoint
	out.Resume = sourceResumeIntent(source, seal, report)
	for _, h := range report.InputHolds {
		out.InputHolds = append(out.InputHolds, privatechat.MigrationInputHold{InboxID: h.InboxID, MessageID: h.MessageID, Reason: h.Reason, PayloadHash: h.PayloadHash})
	}
	if len(out.Checkpoint.QueuedQueueItemIDs) > 0 && out.Resume == nil {
		out.SuspendQueue = true
	}
	out.Records = append(out.Records, privatechat.MigrationImportedRecord{Table: "migration_manifest", Key: seal.SourceHash, ScopeID: seal.ScopeID, Payload: append(json.RawMessage(nil), seal.Manifest...), PayloadHash: seal.SourceHash, DataHash: migrationBytesHash(nil)})
	needed := map[string]bool{}
	for _, item := range report.Checkpoint.QueueItems {
		for _, id := range item.SourceMessageIDs {
			needed[id] = true
		}
		if item.SourceMessageID != "" {
			needed[item.SourceMessageID] = true
		}
	}
	for _, image := range report.Checkpoint.StagedImages {
		needed[image.SourceMessageID] = true
	}
	original := map[string]string{}
	pending := map[string]bool{}
	for _, record := range source.Source.Records {
		if record.Table != "raw_ingress" {
			continue
		}
		var raw work.IngressRecord
		if err = json.Unmarshal(record.Payload, &raw); err != nil {
			return out, err
		}
		if raw.Envelope.ChatType != "p2p" || raw.Route.ScopeID != seal.ScopeID {
			continue
		}
		original[raw.Envelope.MessageID] = raw.Envelope.EventID
		if raw.State == work.IngressPending {
			pending[raw.Envelope.EventID] = true
		}
	}
	var appliedEvents, pendingEvents []privatechat.MigrationImportedEvent
	for _, record := range source.Source.Records {
		out.Records = append(out.Records, privatechat.MigrationImportedRecord{Table: record.Table, Key: record.Key, ScopeID: seal.ScopeID, Order: record.SourceOrder, Revision: record.Revision, Payload: append(json.RawMessage(nil), record.Payload...), PayloadHash: migrationBytesHash(record.Payload), Data: append([]byte(nil), record.Data...), DataHash: migrationBytesHash(record.Data)})
		if record.Table != "ingress_observations" {
			continue
		}
		var observation work.IngressObservation
		if err = json.Unmarshal(record.Payload, &observation); err != nil {
			return privatechat.MigrationImportBundle{}, err
		}
		messageID := observation.Envelope.MessageID
		if original[messageID] != observation.Envelope.EventID || (!needed[messageID] && !pending[observation.Envelope.EventID]) {
			continue
		}
		applied := needed[messageID]
		event, e := ingressmapper.Message(observation.Envelope, observation.Gateway.BotGeneration)
		if e != nil {
			return privatechat.MigrationImportBundle{}, e
		}
		proof, e := json.Marshal(historicalMigrationProof{ScopeID: seal.ScopeID, MigrationID: seal.MigrationID, SourceHash: seal.SourceHash, ObservationKey: record.Key})
		if e != nil {
			return privatechat.MigrationImportBundle{}, e
		}
		historical, e := v.router.ImportHistoricalSourceRecord(ctx, v.binding, event, proof)
		if e != nil {
			return privatechat.MigrationImportBundle{}, e
		}
		target := v.target
		historical.DeliveryTarget = &target
		in, e := v.admitter.BuildPrivateIngress(ctx, historical)
		if errors.Is(e, privatechat.ErrDeferred) {
			action, stageErr := historicalMediaAction(ctx, source, event, v.assetRoot)
			if stageErr != nil {
				return privatechat.MigrationImportBundle{}, fmt.Errorf("historical media %s: %w", event.Key.EventID, stageErr)
			}
			body, stageErr := json.Marshal(action)
			if stageErr != nil {
				return privatechat.MigrationImportBundle{}, stageErr
			}
			if stageErr = v.router.ImportStagedPrivateAction(ctx, event.Key, target, body, proof); stageErr != nil {
				return privatechat.MigrationImportBundle{}, stageErr
			}
			in, e = v.admitter.BuildPrivateIngress(ctx, historical)
		}
		if e != nil {
			return privatechat.MigrationImportBundle{}, fmt.Errorf("canonical historical ingress %s: %w", event.Key.EventID, e)
		}
		if in.Owner != req.Owner || in.Generation != req.Generation {
			return privatechat.MigrationImportBundle{}, privatechat.ErrJournalConflict
		}
		imported := privatechat.MigrationImportedEvent{Ingress: in, SourceScope: seal.ScopeID, SourceKey: record.Key, SourceHash: migrationBytesHash(record.Payload), Applied: applied}
		if applied {
			appliedEvents = append(appliedEvents, imported)
		} else {
			pendingEvents = append(pendingEvents, imported)
		}
		delete(needed, messageID)
		delete(pending, observation.Envelope.EventID)
	}
	if len(needed) != 0 || len(pending) != 0 {
		return privatechat.MigrationImportBundle{}, fmt.Errorf("checkpoint references historical messages without exact SDK observation")
	}
	out.Events = append(appliedEvents, pendingEvents...)
	for _, asset := range assets {
		out.Assets = append(out.Assets, privatechat.MigrationImportedAsset{ContentID: asset.ContentID, SourceMessageID: asset.SourceMessageID, Path: asset.Path, MediaType: asset.MediaType, SHA256: asset.SHA256, Size: asset.Size})
	}
	if err = v.app.verifyPersonalSourceSeal(ctx, v.router, v.binding, seal, v.limits); err != nil {
		return privatechat.MigrationImportBundle{}, err
	}
	return out, nil
}
