package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// privateActionDecoder consumes the frozen result of the legacy SDK parser.
type privateActionDecoder struct{}

func (privateActionDecoder) Decode(entry privatechat.JournalEntry) (control.Action, error) {
	var action control.Action
	err := json.Unmarshal(entry.Ingress.Payload, &action)
	return action, err
}

type privateApplicationAdmitter struct {
	host      *App
	router    *ingressowner.Router
	journal   *privatechat.Journal
	processor *privatechat.Processor
	native    *privateNativeHost
	wake      func()
	refresh   func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error
}

func (a privateApplicationAdmitter) Admit(ctx context.Context, record ingressowner.Record) (ingressowner.AdmissionReceipt, error) {
	var receipt ingressowner.AdmissionReceipt
	if a.host == nil || a.router == nil || a.journal == nil || a.processor == nil || a.wake == nil || record.Owner == nil {
		return receipt, ingressowner.ErrInvalid
	}
	in, err := a.BuildPrivateIngress(ctx, record)
	if err != nil {
		return receipt, &ingressowner.BeforeAdmissionError{Err: err}
	}
	o := in.Owner
	owner := *record.Owner
	if record.DeliveryTarget != nil {
		owner = *record.DeliveryTarget
	}
	seq, _, err := a.journal.Admit(ctx, in)
	if err != nil {
		return receipt, err
	}
	if record.Event.Kind != ingressowner.Card {
		a.wake()
		hash, e := ingressowner.AdmissionHash(record)
		if e != nil {
			return receipt, e
		}
		return ingressowner.AdmissionReceipt{Key: record.Event.Key, Owner: owner, AdmissionID: strconv.FormatInt(seq, 10), PayloadHash: hash}, nil
	}
	refresh := a.refresh
	if refresh == nil {
		refresh = a.host.refreshPrivateBotSettings
	}
	for attempt := 0; attempt < 3; attempt++ {
		err = refresh(ctx, a.processor, o, in.Generation)
		if !errors.Is(err, privatechat.ErrJournalConflict) {
			break
		}
		if ctx.Err() != nil {
			return receipt, ctx.Err()
		}
	}
	if err != nil {
		return receipt, err
	}
	_, e := a.journal.PendingSettingsReconciliations(ctx, o, owner.Epoch, 1)
	if e != nil {
		return receipt, e
	}
	if a.native != nil {
		lease, _ := a.native.settingsLease(ctx, o, owner.Epoch)
		_ = reconcilePrivateBotSettings(ctx, a.host, a.processor, a.journal, lease, o, owner.Epoch)
	}
	needHistory, err := shouldPreparePrivateHistory(ctx, a.processor, o, owner.Epoch, in.Payload)
	if err != nil {
		return receipt, err
	}
	if a.native != nil && needHistory {
		g := applicationIngress{host: a.host, router: a.router}
		scanSeq := int64(0)
		if historyAliasNeedsRefresh(in.Payload) {
			scanSeq = seq
		}
		if err = g.preparePrivateHistory(ctx, a.native, a.processor, o, owner.Epoch, scanSeq); err != nil {
			return receipt, err
		}
	}
	if _, err = a.processor.ApplyIngress(ctx, o, owner.Epoch, seq); err != nil {
		return receipt, err
	}
	paths, err := a.processor.CatalogSnapshot()
	if err != nil {
		return receipt, err
	}
	a.host.SetPrivateCatalogSnapshot(paths)
	a.wake()
	var response json.RawMessage
	var replacement *ingressowner.CallbackReplacement
	if record.Event.Kind == ingressowner.Card {
		card, e := a.processor.ReadCommittedCardResponse(ctx, o, owner.Epoch, seq)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return receipt, e
		}
		if e == nil {
			if card.TargetMessageID != record.Event.MessageID {
				return receipt, ingressowner.ErrConflict
			}
			response = card.Response
			if card.ViewToken != "" {
				sum := sha256.Sum256(card.Response)
				replacement = &ingressowner.CallbackReplacement{TargetMessageID: card.TargetMessageID, Token: card.ViewToken, ResponseHash: hex.EncodeToString(sum[:])}
			}
		}
	}
	hash, err := ingressowner.AdmissionHash(record)
	if err != nil {
		return receipt, err
	}
	return ingressowner.AdmissionReceipt{Key: record.Event.Key, Owner: owner, AdmissionID: strconv.FormatInt(seq, 10), PayloadHash: hash, Response: response, Replacement: replacement}, nil
}

// BuildPrivateIngress is the single canonical SDK-to-private admission builder
// used by live delivery and migration import. It never performs external I/O.
func (a privateApplicationAdmitter) BuildPrivateIngress(ctx context.Context, record ingressowner.Record) (privatechat.Ingress, error) {
	if a.router == nil || record.Owner == nil {
		return privatechat.Ingress{}, ingressowner.ErrInvalid
	}
	owner := *record.Owner
	if record.DeliveryTarget != nil {
		owner = *record.DeliveryTarget
	}
	if owner.Application != ingressowner.Private {
		return privatechat.Ingress{}, ingressowner.ErrConflict
	}
	chat := record.Event.ChatID
	if record.Route != nil && record.Route.Message != nil {
		chat = record.Route.Message.ChatID
	}
	if record.Route != nil && record.Route.Private != nil {
		chat = record.Route.Private.ChatID
	}
	b, err := a.router.PrivateBindingForOwner(ctx, record.Event.Key.Tenant, record.Event.Key.AppID, record.Event.Source.GatewayID, record.Event.Source.BindingGeneration, chat, owner)
	if err != nil {
		return privatechat.Ingress{}, err
	}
	o := privatechat.Owner{GatewayID: b.GatewayID, SurfaceID: gatewaypkg.SurfaceIDForInbound(b.GatewayID, "", "p2p", b.Operator.ID), ChatID: b.ChatID, UserID: b.Operator.ID, TenantID: b.Tenant, AppID: b.AppID}
	var action control.Action
	var ok bool
	switch record.Event.Kind {
	case ingressowner.Message:
		raw, e := ingressmapper.RestoreMessage(record.Event)
		if e != nil {
			return privatechat.Ingress{}, e
		}
		action, ok, e = feishu.DecodePrivateMessage(ctx, raw)
		if errors.Is(e, gatewaypkg.ErrInboundStaging) {
			staged, stageErr := a.router.StagedPrivateAction(ctx, record, owner)
			if errors.Is(stageErr, sql.ErrNoRows) {
				return privatechat.Ingress{}, privatechat.ErrDeferred
			}
			if stageErr != nil {
				return privatechat.Ingress{}, stageErr
			}
			if json.Unmarshal(staged, &action) != nil {
				return privatechat.Ingress{}, ingressowner.ErrConflict
			}
			ok = true
			e = nil
		}
		if e != nil {
			return privatechat.Ingress{}, e
		}
	case ingressowner.Card:
		raw, e := ingressmapper.RestoreCard(record.Event)
		if e != nil {
			return privatechat.Ingress{}, e
		}
		// SDK callbacks may omit chat_id. The resolved message binding above
		// supplies the authenticated target without changing captured facts.
		if raw.ChatID == "" && record.Route != nil && record.Route.Message != nil {
			raw.ChatID = b.ChatID
		}
		action, ok = feishu.DecodePrivateCard(raw, o.SurfaceID)
	case ingressowner.Menu:
		raw, e := ingressmapper.RestoreMenu(record.Event)
		if e != nil {
			return privatechat.Ingress{}, e
		}
		action, ok = feishu.DecodePrivateMenu(raw)
		// A platform-menu event has no chat ID. Its destination is the
		// authenticated p2p binding resolved above, never a historical message
		// or the current navigation state. Keep MessageID empty so this opens a
		// new top-level card just like a slash command.
		action.ChatID = b.ChatID
	case ingressowner.Reaction, ingressowner.Recall:
		raw, e := ingressmapper.RestoreObservation(record.Event)
		if e != nil {
			return privatechat.Ingress{}, e
		}
		action, ok = feishu.DecodePrivateObservation(raw, o.SurfaceID)
	default:
		return privatechat.Ingress{}, ingressowner.ErrInvalid
	}
	if !ok {
		return privatechat.Ingress{}, fmt.Errorf("private ingress requires supported action: %w", privatechat.ErrDeferred)
	}
	if action.GatewayID != o.GatewayID || action.SurfaceSessionID != o.SurfaceID {
		return privatechat.Ingress{}, ingressowner.ErrConflict
	}
	raw, err := json.Marshal(action)
	if err != nil {
		return privatechat.Ingress{}, err
	}
	lane := "control"
	var sources []string
	if record.Event.Kind == ingressowner.Message {
		sources = []string{record.Event.MessageID}
		switch action.Kind {
		case control.ActionTextMessage, control.ActionImageMessage, control.ActionFileMessage:
			lane = "input"
		}
	}
	in := privatechat.Ingress{Owner: o, Generation: owner.Epoch, EventID: record.Event.Key.EventID, Lane: lane, Payload: raw, Sources: sources}
	if record.Sequence > 0 {
		in.OriginRouter, in.OriginSequence = a.router.RouterID(), record.Sequence
	}
	return in, nil

}

// preparePrivateApplicationHost is called during explicit assembly, before any
// gateway hook is installed. It does not activate the production route.
func preparePrivateApplicationHost(host *App, app *privatechat.App, processor *privatechat.Processor) error {
	if host == nil || app == nil || processor == nil {
		return ingressowner.ErrInvalid
	}
	if err := host.BindPrivateServiceOwnership(app); err != nil {
		return err
	}
	paths, err := processor.CatalogSnapshot()
	if err != nil {
		return err
	}
	host.SetPrivateCatalogSnapshot(paths)
	return nil
}
