package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// verifyWorkMembership queries using the same App identity that observed this
// person. Open IDs are never copied into another App's identity namespace.
func (a *App) verifyWorkMembership(ctx context.Context, principalID, scopeID, tenant string, ids work.SenderIDs, gateway work.IngressGateway) error {
	return a.verifyWorkMembershipWithInvitedLeader(ctx, principalID, scopeID, tenant, ids, gateway, false)
}

func (a *App) verifyWorkMembershipWithInvitedLeader(ctx context.Context, principalID, scopeID, tenant string, ids work.SenderIDs, gateway work.IngressGateway, allowInvited bool) (resultErr error) {
	r := a.teamRuntime
	query := workstore.MembershipQuery{ScopeID: scopeID, PrincipalID: principalID, Tenant: tenant}
	identity := work.IdentityScope{Tenant: tenant, AppIdentityDomain: gateway.AppID, IDType: "open_id", ExternalID: ids.OpenID, Generation: gateway.BotGeneration}
	if ids.UserID != "" {
		identity.AppIdentityDomain = "tenant"
		identity.IDType = "user_id"
		identity.ExternalID = ids.UserID
		identity.Generation = 1
	}
	if identity.ExternalID == "" {
		return fmt.Errorf("没有可用于核对群成员的身份，请先在项目群里发一条消息。")
	}
	query.QueriedIdentity = identity
	var binding work.RoleBotBinding
	var op work.Operation
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope(scopeID)
		if e != nil {
			return e
		}
		if scope.Kind != work.ScopeProject {
			return workstore.ErrScopeDenied
		}
		member, e := tx.GetProjectMember(scope.ProjectID, gateway.RoleID)
		if e != nil {
			return e
		}
		if member.State != work.ProjectMemberReady {
			if !allowInvited || member.State != work.ProjectMemberInvited {
				return fmt.Errorf("这位助手还不是该项目的可用成员。")
			}
			project, e := tx.GetProject(scope.ProjectID)
			if e != nil {
				return e
			}
			if project.LeaderRoleID != gateway.RoleID || project.State != work.ProjectProvisioning {
				return fmt.Errorf("这位助手还不是该项目的可用成员。")
			}
		}
		binding, e = tx.GetRoleBotBinding(gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.AppID != gateway.AppID || binding.IdentityGeneration != gateway.BotGeneration {
			return workstore.ErrConflict
		}
		channels, e := tx.ListProjectChannelBindings(scope.ProjectID)
		if e != nil {
			return e
		}
		for _, channel := range channels {
			if channel.Active && channel.Purpose == "main" && channel.Tenant == tenant {
				if query.ChatID != "" {
					return fmt.Errorf("项目群绑定不明确。")
				}
				query.ChatID = channel.ChatID
				query.BindingGeneration = channel.BindingGeneration
			}
		}
		if query.ChatID == "" {
			return workstore.ErrScopeDenied
		}
		actual, e := tx.GetExternalIdentity(identity)
		if e != nil {
			return e
		}
		if actual.PrincipalID != principalID {
			return workstore.ErrScopeDenied
		}
		body, e := json.Marshal(query)
		if e != nil {
			return e
		}
		id := teamID("membership", scopeID+principalID+time.Now().UTC().Format(time.RFC3339Nano))
		content := work.NewContent(id+":request", scopeID, "application/json", body, time.Now().UTC())
		if e = tx.PutContent(content); e != nil {
			return e
		}
		op = work.Operation{ID: id, ScopeID: scopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "membership-verifier"}, Kind: "feishu.membership.query", TargetID: query.ChatID, TargetGeneration: query.BindingGeneration, IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, _, e = tx.PutOperation(op, 0)
		return e
	})
	if err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		persistErr := r.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			current, e := tx.GetOperation(op.ID)
			if e != nil {
				return e
			}
			if current.State != work.OperationSending {
				return nil
			}
			before := current.Revision
			current.Revision++
			current.State = work.OperationTerminal
			current.LastError = "membership query failed: " + resultErr.Error()
			current.UpdatedAt = time.Now().UTC()
			_, _, e = tx.PutOperation(current, before)
			return e
		})
		resultErr = errors.Join(resultErr, persistErr)
	}()
	live, err := (teamDirectory{app: a, store: r.store}).gateway(ctx, binding)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	token := ""
	seen := map[string]bool{}
	present := false
	complete := false
	securityLimited := false
	pages := 0
	for pages < 1000 {
		page, e := live.ProjectChatAPI().ListChatUsersPage(queryCtx, query.ChatID, identity.IDType, token, 100)
		if e != nil {
			return e
		}
		pages++
		securityLimited = securityLimited || page.TriggerSecurityConfLimit
		for _, person := range page.Users {
			if person.ID == identity.ExternalID && (person.IDType == "" || person.IDType == identity.IDType) && (person.TenantKey == "" || person.TenantKey == tenant) {
				present = true
				break
			}
		}
		if present {
			complete = true
			break
		}
		if !page.HasMore {
			complete = !securityLimited
			break
		}
		if seen[page.PageToken] {
			return fmt.Errorf("群成员查询分页没有前进。")
		}
		seen[page.PageToken] = true
		token = page.PageToken
	}
	if !complete {
		return fmt.Errorf("群成员查询结果不完整；尚不能确认此项目的访问范围。")
	}
	observed := time.Now().UTC()
	status := work.MembershipAbsent
	if present {
		status = work.MembershipPresent
	}
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		body, e := json.Marshal(map[string]any{"query": query, "present": present, "complete": complete, "security_limited": securityLimited, "pages": pages, "observed_at": observed})
		if e != nil {
			return e
		}
		content := work.NewContent(op.ID+":response", scopeID, "application/json", body, observed)
		if e = tx.PutContent(content); e != nil {
			return e
		}
		old := op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = content.ID
		op.UpdatedAt = observed
		if _, _, e = tx.PutOperation(op, old); e != nil {
			return e
		}
		return tx.RecordMembershipCheck(work.MembershipCheck{ID: op.ID + ":check", ScopeID: scopeID, PrincipalID: principalID, Gateway: gateway, Tenant: tenant, ChatID: query.ChatID, BindingGeneration: query.BindingGeneration, QueriedIdentity: identity, Status: status, OperationID: op.ID, ResponseRef: content.ID, ObservedAt: observed, ExpiresAt: observed.Add(15 * time.Minute)})
	})
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("当前身份不在这个项目群中。")
	}
	return nil
}
