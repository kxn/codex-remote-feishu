package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type privateModelQueryIntent struct {
	Version                             int
	Owner                               privatechat.Owner
	Generation                          int64
	ProfileID, Fingerprint, TrackingKey string
	Force                               bool
}

type privateModelQueryReceipt struct {
	Version           int
	EffectID, Outcome string
}
type privateModelQueryVerifier struct {
	owner      privatechat.Owner
	generation int64
	id         string
	body       string
}

func (v privateModelQueryVerifier) VerifyEffect(_ context.Context, o privatechat.Owner, g int64, effect privatechat.Effect, receipt json.RawMessage) error {
	if o != v.owner || g != v.generation || effect.ID != v.id || string(receipt) != v.body {
		return privatechat.ErrJournalConflict
	}
	return nil
}

type privateModelQueryReducer struct{}

func (privateModelQueryReducer) ReduceHostReceipt(_ *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	var receipt privateModelQueryReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.EffectID != effect.ID || (receipt.Outcome != "completed" && receipt.Outcome != "stale") {
		return nil, privatechat.ErrJournalConflict
	}
	return nil, nil
}

func runPrivateModelCatalogQuery(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) error {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return err
	}
	if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateModelCatalogQuery {
		return privatechat.ErrJournalConflict
	}
	var intent privateModelQueryIntent
	if json.Unmarshal([]byte(event.DaemonCommand.Text), &intent) != nil || intent.Version != 1 || intent.Owner != o || intent.Generation != generation || intent.ProfileID == "" || intent.Fingerprint == "" || intent.TrackingKey == "" {
		return privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		_, err = p.ApplyLocalHostReceipt(ctx, o, generation, id, privateModelQueryReducer{})
		return err
	}
	if record.State != "prepared" && record.State != "unknown" && record.State != "sending" {
		return privatechat.ErrJournalState
	}
	valid := false
	force := false
	if err = p.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		wait, ok := app.Service().PrivateModelWaiting(o.SurfaceID, intent.TrackingKey)
		valid = ok
		force = intent.Force && wait.ObservationHash == ""
		return nil
	}); err != nil {
		return err
	}
	outcome := "stale"
	if valid {
		observation, e := host.ReadOrRefreshPrivateProfileCatalog(ctx, o, generation, intent.ProfileID, intent.Fingerprint, "", force)
		if e != nil {
			return e
		}
		if observation.Owner != o || observation.Generation != generation || observation.ProfileID != intent.ProfileID || observation.SettingsFingerprint != intent.Fingerprint {
			return privatechat.ErrJournalConflict
		}
		if observation.Snapshot != nil || observation.Unavailable != "" {
			if _, e = p.ApplyPrivateModelCatalog(ctx, o, generation, id, intent.TrackingKey, intent.ProfileID, intent.Fingerprint, observation.Namespace, observation.Revision, observation.Snapshot, observation.Unavailable); e != nil {
				return e
			}
		}
		if observation.Refreshing {
			return privatechat.ErrDeferred
		}
		outcome = "completed"
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(privateModelQueryReceipt{Version: 1, EffectID: id, Outcome: outcome})
	if err != nil {
		return err
	}
	verifier := privateModelQueryVerifier{o, generation, id, string(raw)}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, raw, verifier); err != nil {
		return fmt.Errorf("confirm private model query: %w", err)
	}
	_, err = p.ApplyLocalHostReceipt(ctx, o, generation, id, privateModelQueryReducer{})
	return err
}
