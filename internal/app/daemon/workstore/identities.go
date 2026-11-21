package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrIdentityConflict = errors.New("verified identity evidence conflicts with existing principal histories")

func identityAliases(e work.IdentityEvidence) []work.IdentityScope {
	aliases := []work.IdentityScope{}
	add := func(domain, kind, id string, generation int64) {
		if id != "" {
			aliases = append(aliases, work.IdentityScope{Tenant: e.Tenant, AppIdentityDomain: domain, IDType: kind, ExternalID: id, Generation: generation})
		}
	}
	add("tenant", "user_id", e.IDs.UserID, 1)
	domain := e.AppID
	generation := e.Generation
	if e.UnionDomain != "" {
		domain = "union:" + e.UnionDomain
		generation = 1
	}
	add(domain, "union_id", e.IDs.UnionID, generation)
	add(e.AppID, "open_id", e.IDs.OpenID, e.Generation)
	return aliases
}
func (t *Tx) GetExternalIdentity(identity work.IdentityScope) (work.ExternalIdentity, error) {
	v := work.ExternalIdentity{IdentityScope: identity}
	if err := identity.Validate(); err != nil {
		return v, err
	}
	err := t.tx.QueryRowContext(t.ctx, "SELECT principal_id FROM external_identities WHERE tenant=? AND domain=? AND id_type=? AND external_id=? AND generation=?", identity.Tenant, identity.AppIdentityDomain, identity.IDType, identity.ExternalID, identity.Generation).Scan(&v.PrincipalID)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}
func (t *Tx) ResolvePrincipal(evidence work.IdentityEvidence) (principal work.Principal, created bool, err error) {
	err = t.atomic(func() error {
		if err := evidence.Validate(); err != nil {
			return err
		}
		if _, err := t.GetIngress(evidence.ObservationRef); err != nil {
			return fmt.Errorf("identity evidence must refer to durable SDK ingress: %w", err)
		}
		observations, err := t.ListIngressObservations(evidence.ObservationRef)
		if err != nil {
			return err
		}
		verified := false
		for _, observation := range observations {
			if observation.Envelope.Tenant == evidence.Tenant && observation.Envelope.AppID == evidence.AppID && observation.Envelope.SenderType == "user" && observation.Envelope.SenderIDs == evidence.IDs && observation.Gateway.BotGeneration == evidence.Generation && observation.Gateway.UnionDomain == evidence.UnionDomain {
				verified = true
			}
		}
		if !verified {
			return fmt.Errorf("identity fields are not supported by the persisted SDK observation")
		}
		aliases := identityAliases(evidence)
		owner := ""
		for _, alias := range aliases {
			existing, e := t.GetExternalIdentity(alias)
			if e != nil && !errors.Is(e, ErrNotFound) {
				return e
			}
			if e == nil {
				if owner != "" && owner != existing.PrincipalID {
					return ErrIdentityConflict
				}
				owner = existing.PrincipalID
			}
		}
		if owner == "" {
			sum := sha256.Sum256([]byte(mustJSON(aliases[0])))
			owner = "principal:" + hex.EncodeToString(sum[:])
			principal = work.Principal{ID: owner, DisplayName: aliases[0].ExternalID, Revision: 1}
			if err := t.PutPrincipal(principal, 0); err != nil {
				return err
			}
			created = true
		} else {
			var e error
			principal, e = t.GetPrincipal(owner)
			if e != nil {
				return e
			}
		}
		for _, alias := range aliases {
			_, e := t.tx.ExecContext(t.ctx, "INSERT INTO external_identities(tenant,domain,id_type,external_id,generation,principal_id,evidence_ref) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", alias.Tenant, alias.AppIdentityDomain, alias.IDType, alias.ExternalID, alias.Generation, owner, evidence.ObservationRef)
			if e != nil {
				return e
			}
		}
		return nil
	})
	return
}
