package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishubotidentity"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

// A matching string alone is not an identity. Adoption requires the exact
// authenticated private chat, original Bot application/generation, and one
// unambiguous typed ID present in the new SDK evidence. Groups are never adopted
// as private contexts, and old history never starts a native execution.
func (a *App) adoptTeamLegacyHistory(ctx context.Context, ingressID string) error {
	r := a.teamRuntime
	if r == nil || r.migration == nil {
		return nil
	}
	var ingress work.IngressRecord
	var snapshot workmigration.Snapshot
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		ingress, e = tx.GetIngress(ingressID)
		if e != nil {
			return e
		}
		state, e := tx.GetMigration(teamMigrationID)
		if e != nil {
			return e
		}
		return json.Unmarshal(state.Snapshot, &snapshot)
	})
	if err != nil {
		return err
	}
	raw := ingress.Envelope
	if raw.ChatType != "p2p" || raw.SenderType != "user" {
		return nil
	}
	var identities struct {
		Version int                                 `json:"version"`
		Entries map[string]feishubotidentity.Record `json:"entries"`
	}
	for _, backup := range snapshot.Backups {
		if filepath.Base(backup.Path) == "feishu-bot-identities.json" {
			data, e := readTeamCaptureFile(backup.Path)
			if e != nil {
				return e
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != backup.SHA256 {
				return workstore.ErrConflict
			}
			if json.Unmarshal(data, &identities) != nil {
				return nil
			}
		}
	}
	identity, ok := identities.Entries[raw.GatewayRef]
	if !ok || identities.Version != 1 || identity.Pending != nil || identity.AppID != raw.AppID || int64(identity.Generation) != ingress.Gateway.BotGeneration {
		return nil
	}
	for _, record := range snapshot.Records {
		if record.Kind != "surface" || record.ChatType != "p2p" {
			continue
		}
		var history surfaceresume.Entry
		if json.Unmarshal(record.Raw, &history) != nil {
			continue
		}
		ref, ok := feishuidentity.ParseSurfaceRef(record.UnitKey)
		if !ok || !ref.IsUser() || ref.GatewayID != raw.GatewayRef || history.GatewayID != raw.GatewayRef || history.ChatID != raw.ChatID || history.ActorUserID == "" || ref.ScopeID != history.ActorUserID {
			continue
		}
		alias, ok := teamLegacyAlias(raw, ingress.Gateway, history.ActorUserID)
		if !ok {
			continue
		}
		var principal string
		err = r.store.Within(ctx, func(tx *workstore.Tx) error {
			existing, e := tx.GetExternalIdentity(alias)
			if e != nil {
				return e
			}
			principal = existing.PrincipalID
			item, e := tx.GetMigrationItem(snapshot.ID, record.Key)
			if e != nil {
				return e
			}
			if item.State == "imported" {
				return nil
			}
			return tx.VerifyMigrationIdentity(existing, ingress.ID)
		})
		if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrScopeDenied) {
			continue
		}
		if err != nil {
			return err
		}
		if _, err = r.migration.Adopt(ctx, snapshot.ID, record.Key, workmigration.VerifiedOwner{Identity: alias, PrincipalID: principal, RoleID: ingress.Gateway.RoleID, EvidenceRef: ingress.ID}); err != nil {
			return err
		}
	}
	return nil
}

func teamLegacyAlias(raw work.RawIngressEnvelope, gateway work.IngressGateway, oldActor string) (work.IdentityScope, bool) {
	var choices []work.IdentityScope
	add := func(idType, value, domain string, generation int64) {
		if value != "" && value == oldActor {
			choices = append(choices, work.IdentityScope{Tenant: raw.Tenant, AppIdentityDomain: domain, IDType: idType, ExternalID: value, Generation: generation})
		}
	}
	add("open_id", raw.SenderIDs.OpenID, raw.AppID, gateway.BotGeneration)
	add("user_id", raw.SenderIDs.UserID, "tenant", 1)
	domain, generation := raw.AppID, gateway.BotGeneration
	if gateway.UnionDomain != "" {
		domain = "union:" + gateway.UnionDomain
		generation = 1
	}
	add("union_id", raw.SenderIDs.UnionID, domain, generation)
	if len(choices) != 1 {
		return work.IdentityScope{}, false
	}
	return choices[0], choices[0].Validate() == nil
}
