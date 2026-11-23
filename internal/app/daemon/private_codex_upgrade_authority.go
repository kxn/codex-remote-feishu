package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// privateCodexUpgradeAuthority admits only the durable execute effect created
// by the matching owner's confirmed Codex upgrade card.
type privateCodexUpgradeAuthority struct{ processor *privatechat.Processor }

func (a privateCodexUpgradeAuthority) WithPrivateUpgradeRequest(ctx context.Context, req workupgrade.PrivateRequest, commit func() error) error {
	if a.processor == nil || commit == nil {
		return privatechat.ErrJournalConflict
	}
	s := req.Source
	o := privatechat.Owner{TenantID: s.Tenant, AppID: s.AppID, GatewayID: s.Gateway, SurfaceID: s.Surface, ChatID: s.Chat, UserID: s.User}
	if req.ID != "private-codex:"+s.EffectID {
		return privatechat.ErrJournalConflict
	}
	return a.processor.InspectPendingHostIntent(ctx, o, s.Generation, s.EffectID, func(app *privatechat.App, effect privatechat.EffectRecord) error {
		event, _, err := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if err != nil {
			return err
		}
		cmd := event.DaemonCommand
		flow := app.Service().PrivateUpgradeFlow(o.SurfaceID)
		if cmd == nil || cmd.Kind != control.DaemonCommandUpgradeOwnerFlow || cmd.OptionID != privateUpgradeExecute || cmd.SurfaceSessionID != o.SurfaceID || flow == nil || flow.ID != cmd.PickerID || flow.OwnerUserID != o.UserID || flow.Source != "codex" || flow.Phase != "running" || flow.TargetVersion != req.TargetVersion || flow.StateFingerprint != req.InstallationFingerprint {
			return privatechat.ErrJournalConflict
		}
		return commit()
	})
}
