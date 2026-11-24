package daemon

import (
	"context"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// privateProfileCatalogObservation belongs to the frozen settings identity,
// not to whichever native instance happens to be attached to the surface.
type privateProfileCatalogObservation struct {
	Owner                                     privatechat.Owner
	Generation                                int64
	ProfileID, SettingsFingerprint, Namespace string
	Revision                                  int64
	Snapshot                                  *agentproto.ModelCatalogSnapshot
	Refreshing                                bool
	Unavailable                               string
}

// ReadOrRefreshPrivateProfileCatalog is called outside the processor lock by
// the committed query worker. The shared cache retains its own TTL, credential
// guard and last successful observation.
func (a *App) ReadOrRefreshPrivateProfileCatalog(ctx context.Context, o privatechat.Owner, generation int64, profileID, fingerprint, cursor string, force bool) (privateProfileCatalogObservation, error) {
	result := privateProfileCatalogObservation{Owner: o, Generation: generation, ProfileID: profileID, SettingsFingerprint: fingerprint}
	if a.applications == nil || a.applications.ingress == nil || a.applications.journal == nil || a.teamRuntime == nil || fingerprint == "" {
		return result, privatechat.ErrJournalState
	}
	binding, err := a.applications.ingress.privatePublicationBindingFor(ctx, o, generation)
	if err != nil {
		return result, err
	}
	settings, err := a.readPrivateBotSettings(o.GatewayID, o.AppID)
	if err != nil {
		return result, err
	}
	cp, err := a.applications.journal.LoadCheckpoint(ctx, o, generation)
	if err != nil {
		return result, err
	}
	effective := strings.TrimSpace(settings.Record.CodexProfileID)
	if effective == "" {
		effective = cp.Core.CodexProfileID
	}
	if settings.Fingerprint != fingerprint || state.NormalizeCodexProfileID(effective) != profileID {
		return result, privatechat.ErrJournalConflict
	}
	// The legacy cache key includes the deterministic bot role key. Deriving it
	// does not create or require a team role; binding is the private authority.
	target, err := a.withProfileCatalog(ctx, profileCatalogScope{teamID("role", o.GatewayID), o.GatewayID, o.AppID, binding.BindingGeneration}, profileID, cursor, nil)
	if err != nil {
		result.Unavailable = profileCatalogError(err)
		return result, nil
	}
	// Do not publish a directory from a profile selected while resolution was
	// in flight. A new settings receipt must issue a new frozen query.
	current, err := a.readPrivateBotSettings(o.GatewayID, o.AppID)
	if err != nil {
		return result, err
	}
	if current.Fingerprint != fingerprint || current.Revision != settings.Revision {
		return result, privatechat.ErrJournalConflict
	}
	record, err := a.readOrRefreshProfileCatalog(ctx, target, force)
	if err != nil {
		return result, err
	}
	view := profileCatalogObservation(record)
	result.Namespace, result.Revision = target.Key, record.Revision
	result.Snapshot, result.Refreshing, result.Unavailable = view.Snapshot, record.Refreshing, view.Unavailable
	return result, nil
}
