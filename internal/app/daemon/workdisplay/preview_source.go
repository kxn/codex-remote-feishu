package workdisplay

import (
	"database/sql"
	"errors"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivatePreviewSource struct {
	ActorOpenID   string
	WorkspaceRoot string
	ThreadCWD     string
}

// ResolvePreviewSource binds a temporary file grant to the human whose input
// actually entered this turn and the directory in which that worker ran.
func ResolvePreviewSource(tx *workstore.Tx, display workstore.PrivateExecutionDisplay, localHostID string) (PrivatePreviewSource, bool, error) {
	source, err := privatePreviewSource(tx, display, localHostID)
	if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return PrivatePreviewSource{}, false, nil
	}
	return source, source.ActorOpenID != "" && err == nil, err
}

func privatePreviewSource(tx *workstore.Tx, v workstore.PrivateExecutionDisplay, localHostID string) (PrivatePreviewSource, error) {
	var none PrivatePreviewSource
	if v.ReplyToMessageID == "" || localHostID == "" || v.Validate() != nil {
		return none, nil
	}
	b := v.Binding
	turn, err := tx.GetNativeTurn(v.Incarnation, v.SessionID, v.TurnID)
	if err != nil || turn.Binding != b {
		return none, err
	}
	scope, err := tx.GetScope(b.ScopeID)
	if err != nil || scope.Kind != work.ScopePersonal || scope.RoleID != b.RoleID {
		return none, err
	}
	wc, err := tx.GetContext(b.ContextID)
	if err != nil || wc.ScopeID != scope.ID || wc.RoleID != scope.RoleID || wc.Purpose == work.PurposeReview || wc.Purpose == work.PurposeControl {
		return none, err
	}
	bot, err := tx.GetRoleBotBinding(b.RoleID)
	if err != nil || bot != v.Bot {
		return none, err
	}
	attempt, err := tx.ValidateNativeBinding(b)
	if err != nil {
		return none, err
	}
	dest, messageID, err := destinationForAttempt(tx, scope, attempt, bot)
	if err != nil || messageID != v.ReplyToMessageID || dest.ChatID != v.ChatID || dest.Tenant != v.Tenant {
		return none, err
	}
	raw, err := tx.GetIngressByPlatformMessage(v.Tenant, v.ChatID, messageID)
	if err != nil {
		return none, err
	}
	env := raw.Envelope
	if raw.State != work.IngressProcessed || raw.Route.Kind != work.ScopePersonal ||
		raw.Gateway.RoleID != bot.RoleID || raw.Gateway.BotGeneration != bot.IdentityGeneration ||
		env.GatewayRef != bot.GatewayRef || env.AppID != bot.AppID || env.ChatType != "p2p" ||
		env.SenderType != "user" || env.SenderIDs.OpenID == "" {
		return none, nil
	}
	msg, err := tx.GetMessage(raw.MessageID)
	if err != nil || msg.ScopeID != scope.ID || msg.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) ||
		msg.PlatformKey == nil || *msg.PlatformKey != (work.MessageKey{Tenant: v.Tenant, ChatID: v.ChatID, MessageID: messageID}) {
		return none, err
	}
	recalled, err := tx.IsPlatformMessageRecalled(v.Tenant, v.ChatID, messageID)
	if err != nil || recalled {
		return none, err
	}
	launch, err := tx.GetNativeLaunchSnapshot(b.WorkerSessionID, b.SessionGeneration)
	if err != nil || !launch.Owner.SameOwner(b) || launch.CarrierIncarnation != v.Incarnation ||
		launch.HostID != localHostID || !filepath.IsAbs(launch.CWD) {
		return none, err
	}
	return PrivatePreviewSource{ActorOpenID: env.SenderIDs.OpenID, WorkspaceRoot: launch.CWD, ThreadCWD: launch.CWD}, nil
}
