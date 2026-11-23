package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamGroupWitnessVerifier struct{ store *workstore.Store }
type teamGroupWitnessProof struct {
	IngressID string `json:"ingress_id"`
	Revision  int64  `json:"revision"`
	FactHash  string `json:"fact_hash"`
}

func (v teamGroupWitnessVerifier) VerifyGroupOwnerWitness(ctx context.Context, w ingressowner.GroupOwnerWitness, raw []byte) error {
	var proof teamGroupWitnessProof
	if v.store == nil || json.Unmarshal(raw, &proof) != nil || proof.IngressID == "" || proof.Revision < 1 || proof.FactHash != w.SourceFactHash {
		return ingressowner.ErrInvalid
	}
	return v.store.Within(ctx, func(tx *workstore.Tx) error {
		record, obs, err := tx.GroupOwnerObservation(w.SourceEvent.AppID, w.SourceEvent.EventID, w.BindingGeneration)
		if err != nil {
			return err
		}
		bot, err := tx.GetRoleBotBinding(obs.Gateway.RoleID)
		if err != nil {
			return err
		}
		if record.ID != proof.IngressID || record.Revision < proof.Revision || obs.Gateway.GatewayRef != w.GatewayID || obs.Gateway.AppID != w.SourceEvent.AppID || obs.Gateway.BotGeneration != w.BindingGeneration || bot.GatewayRef != w.GatewayID || bot.AppID != w.SourceEvent.AppID || bot.IdentityGeneration != w.BindingGeneration || obs.Envelope.Tenant != w.SourceEvent.Tenant || obs.Envelope.ChatID != w.ChatID || obs.Envelope.EventID != w.SourceEvent.EventID {
			return workstore.ErrScopeDenied
		}
		event, err := ingressmapper.Message(obs.Envelope, w.BindingGeneration)
		if err != nil {
			return err
		}
		hash, err := ingressowner.EventFactHash(event)
		if err != nil {
			return err
		}
		if hash != w.SourceFactHash {
			return workstore.ErrConflict
		}
		return nil
	})
}

// PrepareHistoricalTeamOwners imports only witnessed group identities before
// any gateway starts. Missing/obsolete identity evidence remains unresolved.
func PrepareHistoricalTeamOwners(ctx context.Context, router *ingressowner.Router, store *workstore.Store) error {
	if router == nil || store == nil {
		return fmt.Errorf("group witness dependencies required")
	}
	verifier := teamGroupWitnessVerifier{store: store}
	router.SetGroupOwnerWitnessVerifier(verifier)
	for offset := 0; ; offset += 100 {
		var observations []work.IngressObservation
		if err := store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			observations, e = tx.GroupOwnerObservations(100, offset)
			return e
		}); err != nil {
			return err
		}
		if len(observations) == 0 {
			return nil
		}
		for _, obs := range observations {
			if err := ctx.Err(); err != nil {
				return err
			}
			event, err := ingressmapper.Message(obs.Envelope, obs.Gateway.BotGeneration)
			if err != nil {
				continue
			}
			hash, err := ingressowner.EventFactHash(event)
			if err != nil {
				continue
			}
			var record work.IngressRecord
			err = store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				record, _, e = tx.GroupOwnerObservation(obs.Gateway.AppID, obs.Envelope.EventID, obs.Gateway.BotGeneration)
				return e
			})
			if err != nil {
				continue
			}
			witness := ingressowner.GroupOwnerWitness{SourceEvent: event.Key, SourceFactHash: hash, GatewayID: obs.Gateway.GatewayRef, BindingGeneration: obs.Gateway.BotGeneration, ChatID: event.ChatID}
			proof, _ := json.Marshal(teamGroupWitnessProof{IngressID: record.ID, Revision: record.Revision, FactHash: hash})
			if _, err = router.ImportHistoricalGroupOwner(ctx, witness, proof); err != nil {
				continue
			}
		}
		if len(observations) < 100 {
			return nil
		}
	}
}
