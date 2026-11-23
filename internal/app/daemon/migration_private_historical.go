package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type historicalMigrationProof struct{ ScopeID, MigrationID, SourceHash, ObservationKey string }
type teamHistoricalMigrationVerifier struct {
	app       *App
	router    *ingressowner.Router
	assetRoot string
	limits    workstore.PersonalExportLimits
}

// localSource is invoked by the neutral registry while its owner lock is held.
// It must never call back into the registry's bound-drain verifier.
func (v teamHistoricalMigrationVerifier) localSource(ctx context.Context, event ingressowner.Event, proof []byte) (PersonalMigrationSource, historicalMigrationProof, error) {
	var source PersonalMigrationSource
	var ref historicalMigrationProof
	if v.app == nil || v.app.teamRuntime == nil || json.Unmarshal(proof, &ref) != nil || ref.ScopeID == "" || ref.MigrationID == "" || ref.SourceHash == "" || ref.ObservationKey == "" {
		return source, ref, workstore.ErrScopeDenied
	}
	var seal workstore.PersonalMigrationSeal
	if err := v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(ref.ScopeID, ref.MigrationID)
		return e
	}); err != nil {
		return source, ref, err
	}
	if seal.SourceHash != ref.SourceHash {
		return source, ref, workstore.ErrConflict
	}
	if err := v.app.verifyPersonalSourceSealLocal(ctx, seal, v.limits); err != nil {
		return source, ref, err
	}
	if err := json.Unmarshal(seal.Manifest, &source); err != nil {
		return source, ref, err
	}
	found := false
	for _, record := range source.Source.Records {
		if record.Table != "ingress_observations" || record.Key != ref.ObservationKey {
			continue
		}
		var observation work.IngressObservation
		if err := json.Unmarshal(record.Payload, &observation); err != nil {
			return source, ref, err
		}
		actual, err := ingressmapper.Message(observation.Envelope, observation.Gateway.BotGeneration)
		if err != nil {
			return source, ref, err
		}
		a, err := ingressowner.EventFactHash(actual)
		if err != nil {
			return source, ref, err
		}
		b, err := ingressowner.EventFactHash(event)
		if err != nil {
			return source, ref, err
		}
		if actual.Key != event.Key || a != b {
			return source, ref, workstore.ErrConflict
		}
		found = true
	}
	if !found {
		return source, ref, workstore.ErrNotFound
	}
	return source, ref, nil
}
func (v teamHistoricalMigrationVerifier) VerifyHistoricalSource(ctx context.Context, binding ingressowner.OwnerScopeBinding, event ingressowner.Event, proof []byte) error {
	_, ref, err := v.localSource(ctx, event, proof)
	if err != nil {
		return err
	}
	if ref.ScopeID != binding.ScopeID {
		return workstore.ErrScopeDenied
	}
	return nil
}
func historicalMediaAction(ctx context.Context, source PersonalMigrationSource, event ingressowner.Event, assetRoot string) (control.Action, error) {
	var zero control.Action
	assets, err := materializePersonalMigrationAssets(source, assetRoot)
	if err != nil {
		return zero, err
	}
	raw, err := ingressmapper.RestoreMessage(event)
	if err != nil {
		return zero, err
	}
	action, ok, err := feishu.DecodeHistoricalPrivateMedia(ctx, raw, func(message, key, name string) (string, string, error) {
		var selected *PersonalMigrationAsset
		for n := range assets {
			a := &assets[n]
			if a.SourceMessageID != message {
				continue
			}
			if selected != nil {
				return "", "", workstore.ErrConflict
			}
			selected = a
		}
		if selected == nil || key == "" || (selected.Kind == "file" && selected.FileName != name) {
			return "", "", workstore.ErrScopeDenied
		}
		return selected.Path, selected.MediaType, nil
	})
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, workstore.ErrConflict
	}
	return action, nil
}
func (v teamHistoricalMigrationVerifier) VerifyHistoricalPrivateStaging(ctx context.Context, record ingressowner.Record, target ingressowner.Owner, body json.RawMessage, proof []byte) error {
	if record.Owner == nil || v.router == nil {
		return workstore.ErrScopeDenied
	}
	source, ref, err := v.localSource(ctx, record.Event, proof)
	if err != nil {
		return err
	}
	var seal workstore.PersonalMigrationSeal
	if err = v.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		seal, e = tx.GetPersonalMigrationSeal(ref.ScopeID, ref.MigrationID)
		return e
	}); err != nil {
		return err
	}
	var evidence personalSourceSealEvidence
	if json.Unmarshal(seal.Evidence, &evidence) != nil || evidence.Neutral.Owner != *record.Owner {
		return workstore.ErrScopeDenied
	}
	binding, err := v.router.PrivateBindingForOwner(ctx, record.Event.Key.Tenant, record.Event.Key.AppID, record.Event.Source.GatewayID, record.Event.Source.BindingGeneration, record.Event.ChatID, target)
	if err != nil {
		return err
	}
	action, err := historicalMediaAction(ctx, source, record.Event, v.assetRoot)
	if err != nil {
		return fmt.Errorf("historical media staging: %w", err)
	}
	if binding.Operator.ID != action.ActorUserID || binding.ChatID != action.ChatID {
		return workstore.ErrScopeDenied
	}
	expected, err := json.Marshal(action)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, body) {
		return workstore.ErrConflict
	}
	return nil
}
