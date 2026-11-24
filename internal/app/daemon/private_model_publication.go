package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateModelViewAuthority interface {
	CheckPrivateModelView(context.Context, privatechat.Owner, int64, string, string, string) (bool, bool, error)
}

// modelPublicationTarget uses the already committed callback response or the
// previous page's actual publication receipt, never caller supplied IDs.
func (p *privatePublisher) modelPublicationTarget(ctx context.Context, o privatechat.Owner, generation int64, id string, event *eventcontract.Event, predecessor *privatechat.PublicationReceipt) (bool, error) {
	if event.PageView == nil || !event.PageView.PrivateModelResult {
		return false, nil
	}
	var wait orchestrator.PrivateModelWait
	var current bool
	err := p.processor.InspectPendingHostIntent(ctx, o, generation, id, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		wait, current = app.Service().PrivateModelWaiting(o.SurfaceID, event.PageView.TrackingKey)
		return nil
	})
	if err != nil {
		return false, err
	}
	if !current {
		return true, nil
	}
	if wait.ProfileID != "" {
		obs, e := p.host.ReadOrRefreshPrivateProfileCatalog(ctx, o, generation, wait.ProfileID, wait.SettingsFingerprint, "", false)
		if errors.Is(e, privatechat.ErrJournalConflict) {
			return true, nil
		}
		if e != nil {
			return false, e
		}
		if event.PageView.PrivateCatalogNamespace != "" && obs.Namespace != event.PageView.PrivateCatalogNamespace {
			return true, nil
		}
	}
	message, token := wait.MessageID, wait.ViewToken
	if token == "" && predecessor != nil {
		for _, result := range predecessor.Results {
			var receipt feishu.PrivateOperationReceipt
			if json.Unmarshal(result.Receipt, &receipt) != nil {
				return false, privatechat.ErrJournalConflict
			}
			if receipt.Kind != feishu.OperationSendCard && receipt.Kind != feishu.OperationUpdateCard {
				continue
			}
			_, viewToken, e := feishu.UnstampPrivateView(result.Operation)
			if e != nil {
				return false, e
			}
			message, token = receipt.MessageID, viewToken
		}
	}
	if message == "" || token == "" {
		return false, privatechat.ErrDisplayHeld
	}
	verifier, ok := p.ownership.(privateModelViewAuthority)
	if !ok {
		return false, privatechat.ErrJournalState
	}
	sourceEventID := ""
	if wait.SourceSequence != 0 {
		entry, e := p.journal.ReadIngressAction(ctx, o, generation, wait.SourceSequence)
		if e != nil {
			return false, e
		}
		sourceEventID = entry.Ingress.EventID
	}
	known, valid, err := verifier.CheckPrivateModelView(ctx, o, generation, message, token, sourceEventID)
	if err != nil {
		return false, err
	}
	if !known {
		return false, privatechat.ErrDisplayHeld
	}
	if !valid {
		return true, nil
	}
	event.PageView.MessageID, event.PageView.PrivateExpectedViewToken = message, token
	event.Payload = nil
	*event = event.Normalized()
	return false, nil
}
