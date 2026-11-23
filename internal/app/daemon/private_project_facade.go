package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type privateProjectAuthority struct {
	Owner     privatechat.Owner
	Epoch     int64
	Binding   ingressowner.PrivateBinding
	Principal work.Principal
	Gateway   work.IngressGateway
	IDs       work.SenderIDs
}
type privateProjectFacade struct {
	app     *App
	router  *ingressowner.Router
	journal *privatechat.Journal
}
type privateProjectItem struct{ ID, Name, State string }
type privateProjectPage struct {
	Items     []privateProjectItem
	NextAfter string
}

// authority only recognises a canonical SDK event already accepted by this
// exact private owner. It never accepts identity fields supplied by a model.
func (f privateProjectFacade) authority(ctx context.Context, o privatechat.Owner, epoch int64, privateOwner ingressowner.Owner) (privateProjectAuthority, error) {
	var out privateProjectAuthority
	if f.app == nil || f.app.teamRuntime == nil || f.router == nil || f.journal == nil || privateOwner.Application != ingressowner.Private || privateOwner.Epoch != epoch {
		return out, workstore.ErrScopeDenied
	}
	gateway, err := (teamDirectory{app: f.app, store: f.app.teamRuntime.store}).ResolveGateway(ctx, o.GatewayID, o.AppID)
	if err != nil {
		return out, err
	}
	binding, err := f.router.PrivateBindingForOwner(ctx, o.TenantID, o.AppID, o.GatewayID, gateway.BotGeneration, o.ChatID, privateOwner)
	if err != nil {
		return out, err
	}
	if binding.Operator.ID != o.UserID {
		return out, workstore.ErrScopeDenied
	}
	ids, err := f.journal.RecentEventIDs(ctx, o, epoch, 200)
	if err != nil {
		return out, err
	}
	pending := false
	for _, id := range ids {
		record, e := f.router.Get(ctx, ingressowner.Key{Tenant: o.TenantID, AppID: o.AppID, EventID: id})
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return out, e
		}
		target := record.Owner
		if record.DeliveryTarget != nil {
			target = record.DeliveryTarget
		}
		if target == nil || *target != privateOwner || record.Event.Kind != ingressowner.Message || record.Event.ChatType != "p2p" || record.Event.ChatID != o.ChatID || record.Event.Operator != binding.Operator || record.Event.Source.GatewayID != o.GatewayID || record.Event.Source.BindingGeneration != gateway.BotGeneration {
			continue
		}
		if record.State != ingressowner.Accepted {
			if record.State == ingressowner.Pending || record.State == ingressowner.Delivering {
				pending = true
			}
			continue
		}
		raw, e := ingressmapper.RestoreMessage(record.Event)
		if e != nil {
			return out, e
		}
		if raw.SenderType != "user" || raw.Tenant != o.TenantID || raw.SenderTenant != "" && raw.SenderTenant != raw.Tenant {
			return out, workstore.ErrScopeDenied
		}
		factHash, e := ingressowner.EventFactHash(record.Event)
		if e != nil {
			return out, e
		}
		obs := workstore.PrivateProjectIdentityObservation{Tenant: o.TenantID, AppID: o.AppID, EventID: id, FactHash: factHash, OwnerID: privateOwner.ID, OwnerEpoch: privateOwner.Epoch, Generation: gateway.BotGeneration, UnionDomain: gateway.UnionDomain, IDs: raw.SenderIDs}
		var frozen workstore.PrivateProjectIdentityObservation
		lookupErr := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			frozen, e = tx.PrivateProjectIdentityObservationForEvent(o.TenantID, o.AppID, id)
			return e
		})
		if lookupErr == nil {
			if frozen.FactHash != factHash || frozen.OwnerID != privateOwner.ID || frozen.OwnerEpoch != privateOwner.Epoch || frozen.Generation != gateway.BotGeneration || frozen.UnionDomain != gateway.UnionDomain || frozen.IDs.OpenID != raw.SenderIDs.OpenID || frozen.IDs.UnionID != raw.SenderIDs.UnionID || (raw.SenderIDs.UserID != "" && frozen.IDs.UserID != raw.SenderIDs.UserID) || (frozen.ContactUserID != "" && (frozen.ContactOpenID != raw.SenderIDs.OpenID || frozen.ContactUserID != frozen.IDs.UserID)) {
				return out, workstore.ErrIdentityConflict
			}
			obs = frozen
		} else if !errors.Is(lookupErr, workstore.ErrNotFound) {
			return out, lookupErr
		} else if obs.IDs.UserID == "" && obs.IDs.OpenID != "" {
			// Contact lookup is performed using this exact Bot's authenticated
			// gateway. An unavailable API leaves the identity application-scoped.
			var binding work.RoleBotBinding
			if e := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var x error; binding, x = tx.GetRoleBotBinding(gateway.RoleID); return x }); e == nil {
				if live, e := (teamDirectory{app: f.app, store: f.app.teamRuntime.store}).gateway(ctx, binding); e == nil {
					queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					userID, e := live.ProjectChatAPI().ResolveContactUserID(queryCtx, obs.IDs.OpenID)
					cancel()
					if e == nil {
						obs.ContactOpenID, obs.ContactUserID = obs.IDs.OpenID, userID
						obs.IDs.UserID = userID
					}
				}
			}
		}
		var principal work.Principal
		e = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			var x error
			principal, x = tx.ResolvePrivateProjectIdentity(obs)
			return x
		})
		if errors.Is(e, workstore.ErrIdentityConflict) && errors.Is(lookupErr, workstore.ErrNotFound) && obs.IDs.UserID != "" {
			// An earlier observation may have created an app-only principal
			// before contact lookup became available. Do not rewrite its history:
			// resolve this new, verified observation through the tenant ID alone.
			obs.AliasIDs = work.SenderIDs{UserID: obs.IDs.UserID}
			e = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
				var x error
				principal, x = tx.ResolvePrivateProjectIdentity(obs)
				return x
			})
		}
		if e != nil {
			return out, e
		}
		return privateProjectAuthority{Owner: o, Epoch: epoch, Binding: binding, Principal: principal, Gateway: gateway, IDs: obs.IDs}, nil
	}
	if pending {
		return out, privatechat.ErrDeferred
	}
	return out, fmt.Errorf("no accepted private SDK identity observation: %w", workstore.ErrNotFound)
}

// projects checks current access in the source transaction; expired grants can
// only be refreshed by the real SDK membership verifier outside that transaction.
func (f privateProjectFacade) projects(ctx context.Context, a privateProjectAuthority, after string, limit int) (privateProjectPage, error) {
	var out privateProjectPage
	if limit < 1 || limit > 100 {
		return out, fmt.Errorf("invalid page size")
	}
	var candidates []work.Project
	err := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; candidates, e = tx.ListProjects(); return e })
	if err != nil {
		return out, err
	}
	for _, project := range candidates {
		if project.ID <= after {
			continue
		}
		var member bool
		var accessErr error
		err = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			binding, e := tx.GetRoleBotBinding(a.Gateway.RoleID)
			if e != nil || binding.AppID != a.Gateway.AppID || binding.GatewayRef != a.Gateway.GatewayRef || binding.IdentityGeneration != a.Gateway.BotGeneration {
				return workstore.ErrScopeDenied
			}
			m, e := tx.GetProjectMember(project.ID, a.Gateway.RoleID)
			if e != nil {
				if errors.Is(e, workstore.ErrNotFound) {
					return nil
				}
				return e
			}
			member = m.State == work.ProjectMemberReady && m.BotGeneration == a.Gateway.BotGeneration
			if !member {
				return nil
			}
			accessErr = tx.RequireProjectPrincipalAccess(project.ScopeID, a.Principal.ID, time.Now().UTC())
			return nil
		})
		if err != nil {
			return out, err
		}
		if !member {
			continue
		}
		if accessErr != nil {
			if e := f.app.verifyWorkMembership(ctx, a.Principal.ID, project.ScopeID, a.Owner.TenantID, a.IDs, a.Gateway); e != nil {
				continue
			}
			err = f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
				return tx.RequireProjectPrincipalAccess(project.ScopeID, a.Principal.ID, time.Now().UTC())
			})
			if err != nil {
				continue
			}
		}
		if len(out.Items) == limit {
			out.NextAfter = out.Items[len(out.Items)-1].ID
			break
		}
		out.Items = append(out.Items, privateProjectItem{ID: project.ID, Name: project.Name, State: string(project.State)})
	}
	return out, nil
}

type privateProjectResource struct {
	ID, Purpose, Connector string
	Revision               int64
}
type privateProjectResourcePage struct {
	Project   privateProjectItem
	Items     []privateProjectResource
	NextAfter string
}
type privateProjectVersion struct {
	ResourceID, VersionID, Content, ContentHash, MediaType string
	Complete, Saved                                        bool
}

func (f privateProjectFacade) authorizedProject(tx *workstore.Tx, a privateProjectAuthority, projectID string) (work.Project, error) {
	p, err := tx.GetProject(projectID)
	if err != nil {
		return p, err
	}
	if err = tx.RequireProjectPrincipalAccess(p.ScopeID, a.Principal.ID, time.Now().UTC()); err != nil {
		return p, err
	}
	binding, err := tx.GetRoleBotBinding(a.Gateway.RoleID)
	if err != nil || binding.AppID != a.Gateway.AppID || binding.GatewayRef != a.Gateway.GatewayRef || binding.IdentityGeneration != a.Gateway.BotGeneration {
		return p, workstore.ErrScopeDenied
	}
	member, err := tx.GetProjectMember(p.ID, a.Gateway.RoleID)
	if err != nil {
		return p, err
	}
	if member.State != work.ProjectMemberReady || member.BotGeneration != a.Gateway.BotGeneration {
		return p, workstore.ErrScopeDenied
	}
	return p, nil
}
func (f privateProjectFacade) resources(ctx context.Context, a privateProjectAuthority, projectID, after string, limit int) (privateProjectResourcePage, error) {
	var out privateProjectResourcePage
	if limit < 1 || limit > 100 {
		return out, fmt.Errorf("invalid page size")
	}
	err := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		p, e := f.authorizedProject(tx, a, projectID)
		if e != nil {
			return e
		}
		out.Project = privateProjectItem{ID: p.ID, Name: p.Name, State: string(p.State)}
		items, e := tx.ListResources(p.ScopeID)
		if e != nil {
			return e
		}
		for _, r := range items {
			if r.ID <= after {
				continue
			}
			if len(out.Items) == limit {
				out.NextAfter = out.Items[len(out.Items)-1].ID
				break
			}
			out.Items = append(out.Items, privateProjectResource{ID: r.ID, Purpose: r.Purpose, Connector: r.ConnectorIdentity, Revision: r.Revision})
		}
		return nil
	})
	return out, err
}
func (f privateProjectFacade) readVersion(ctx context.Context, a privateProjectAuthority, projectID, resourceID, versionID string) (privateProjectVersion, error) {
	var out privateProjectVersion
	err := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		p, e := f.authorizedProject(tx, a, projectID)
		if e != nil {
			return e
		}
		resource, e := tx.GetResource(p.ScopeID, resourceID)
		if e != nil {
			return e
		}
		version, e := tx.GetResourceVersion(p.ScopeID, versionID)
		if e != nil {
			return e
		}
		if version.ResourceID != resource.ID {
			return workstore.ErrScopeDenied
		}
		out = privateProjectVersion{ResourceID: resource.ID, VersionID: version.ID, ContentHash: version.ContentHash, Complete: version.Complete, Saved: version.Saved}
		if version.ContentRef == "" {
			return nil
		}
		body, e := tx.GetContent(p.ScopeID, version.ContentRef)
		if e != nil {
			return e
		}
		if body.SHA256 != version.ContentHash || len(body.Data) > 64<<10 || !utf8.Valid(body.Data) || !strings.HasPrefix(body.MediaType, "text/") {
			return fmt.Errorf("saved resource is not available as bounded text")
		}
		out.Content = string(body.Data)
		out.MediaType = body.MediaType
		return nil
	})
	return out, err
}

func (f privateProjectFacade) versions(ctx context.Context, a privateProjectAuthority, projectID, resourceID, after string, limit int) ([]privateProjectVersion, string, error) {
	if limit < 1 || limit > 100 {
		return nil, "", fmt.Errorf("invalid page size")
	}
	var out []privateProjectVersion
	next := ""
	err := f.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		p, e := f.authorizedProject(tx, a, projectID)
		if e != nil {
			return e
		}
		if _, e = tx.GetResource(p.ScopeID, resourceID); e != nil {
			return e
		}
		versions, e := tx.ListResourceVersions(p.ScopeID, resourceID)
		if e != nil {
			return e
		}
		for _, v := range versions {
			if v.ID <= after {
				continue
			}
			if len(out) == limit {
				next = out[len(out)-1].VersionID
				break
			}
			out = append(out, privateProjectVersion{ResourceID: resourceID, VersionID: v.ID, ContentHash: v.ContentHash, Complete: v.Complete, Saved: v.Saved})
		}
		return nil
	})
	return out, next, err
}
