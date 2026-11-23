package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

type teamApplicationAdmitter struct{ host *App }

func (a teamApplicationAdmitter) Admit(ctx context.Context, record ingressowner.Record) (ingressowner.AdmissionReceipt, error) {
	var result ingressowner.AdmissionReceipt
	if a.host == nil || a.host.teamRuntime == nil || record.Owner == nil {
		return result, ingressowner.ErrInvalid
	}
	owner := *record.Owner
	if record.DeliveryTarget != nil {
		owner = *record.DeliveryTarget
	}
	if owner.Application != ingressowner.Team {
		return result, ingressowner.ErrConflict
	}
	r := a.host.teamRuntime
	id := record.Event.Key.EventID
	var response json.RawMessage
	switch record.Event.Kind {
	case ingressowner.Message:
		raw, err := ingressmapper.RestoreMessage(record.Event)
		if err != nil {
			return result, err
		}
		receipt, err := r.ingress.Receive(ctx, raw)
		if err != nil {
			return result, err
		}
		if err = a.host.adoptTeamLegacyHistory(ctx, receipt.IngressID); err != nil {
			return result, err
		}
		id = receipt.IngressID
		r.wakeLane("ingress")
	case ingressowner.Card:
		raw, err := ingressmapper.RestoreCard(record.Event)
		if err != nil {
			return result, err
		}
		text, err := a.host.receiveTeamInteraction(ctx, raw)
		if err != nil {
			return result, err
		}
		response, err = json.Marshal(map[string]any{"toast": map[string]any{"type": "info", "content": text}})
		if err != nil {
			return result, err
		}
	case ingressowner.Reaction, ingressowner.Recall, ingressowner.GroupMember:
		raw, err := ingressmapper.RestoreObservation(record.Event)
		if err != nil {
			return result, err
		}
		if err = a.host.receiveTeamPlatformEvent(ctx, raw); err != nil {
			return result, err
		}
	default:
		return result, fmt.Errorf("unsupported team ingress kind")
	}
	hash, err := ingressowner.AdmissionHash(record)
	if err != nil {
		return result, err
	}
	return ingressowner.AdmissionReceipt{Key: record.Event.Key, Owner: owner, AdmissionID: id, PayloadHash: hash, Response: response}, nil
}
