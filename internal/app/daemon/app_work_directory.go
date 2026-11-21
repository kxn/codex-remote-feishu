package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type teamDirectory struct {
	app   *App
	store *workstore.Store
}

func (d teamDirectory) gateway(ctx context.Context, binding work.RoleBotBinding) (*feishu.LiveGateway, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var current work.RoleBotBinding
	err := d.store.Within(ctx, func(tx *workstore.Tx) error { var e error; current, e = tx.GetRoleBotBinding(binding.RoleID); return e })
	if err != nil {
		return nil, err
	}
	if current != binding {
		return nil, fmt.Errorf("role bot identity changed")
	}
	controller, ok := d.app.gateway.(*feishu.MultiGatewayController)
	if !ok {
		return nil, fmt.Errorf("configured multi-application controller required")
	}
	return controller.ProjectGateway(binding.GatewayRef, binding.AppID)
}

func (d teamDirectory) ResolveGateway(ctx context.Context, gatewayRef, appID string) (work.IngressGateway, error) {
	var result work.IngressGateway
	loaded, err := d.app.loadAdminConfig()
	if err != nil {
		return result, err
	}
	configured := false
	for _, app := range d.app.runtimeGatewayApps(loaded.Config) {
		if app.GatewayID == gatewayRef && app.AppID == appID && app.Enabled {
			configured = true
			break
		}
	}
	if !configured {
		return result, fmt.Errorf("receiving application is no longer enabled")
	}
	err = d.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, e := tx.GetRoleBotBinding(teamID("role", gatewayRef))
		if e != nil {
			return e
		}
		if binding.AppID != appID {
			return fmt.Errorf("receiving application identity changed")
		}
		result = work.IngressGateway{GatewayRef: binding.GatewayRef, AppID: binding.AppID, RoleID: binding.RoleID, BotGeneration: binding.IdentityGeneration}
		return nil
	})
	if err != nil {
		return result, err
	}
	result.BotOpenID = d.app.feishuFactBotOpenID(gatewayRef)
	// No inferred cross-application union realm. Equal open_id text from
	// different applications is not proof of one human principal.
	return result, nil
}

func (d teamDirectory) ResolveLeader(ctx context.Context, tenant, chat, observingRole string) (string, error) {
	leader := ""
	err := d.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, e := tx.GetProjectChannelBinding(tenant, chat)
		if e == nil && binding.Active {
			project, e := tx.GetProject(binding.ProjectID)
			leader = project.LeaderRoleID
			return e
		}
		if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		teams, e := tx.ListTeams()
		if e != nil {
			return e
		}
		leaders := map[string]bool{}
		for _, team := range teams {
			member, e := tx.GetTeamMember(team.ID, team.LeaderRoleID)
			if e == nil && member.Active {
				leaders[team.LeaderRoleID] = true
			}
		}
		if len(leaders) == 1 {
			for id := range leaders {
				leader = id
			}
			return nil
		}
		return fmt.Errorf("请明确 @ 一位负责人，或使用负责人选择入口建立项目")
	})
	return leader, err
}

func (d teamDirectory) ResolveMention(ctx context.Context, gateway work.IngressGateway, mention work.RawMention) (string, error) {
	if mention.IDs.OpenID == "" {
		return "", nil
	}
	var bindings []work.RoleBotBinding
	if err := d.store.Within(ctx, func(tx *workstore.Tx) error { var e error; bindings, e = tx.ListRoleBotBindings(); return e }); err != nil {
		return "", err
	}
	role := ""
	for _, binding := range bindings {
		id := d.app.feishuFactBotOpenID(binding.GatewayRef)
		if id != "" && id == mention.IDs.OpenID {
			if role != "" && role != binding.RoleID {
				return "", fmt.Errorf("mentioned bot identity is ambiguous")
			}
			role = binding.RoleID
		}
	}
	return role, nil
}

func (d teamDirectory) authorize(ctx context.Context, envelope work.RawIngressEnvelope) (bool, error) {
	if err := envelope.Validate(); err != nil {
		return false, err
	}
	if _, err := d.ResolveGateway(ctx, envelope.GatewayRef, envelope.AppID); err != nil {
		return false, err
	}
	if envelope.SenderType == "user" {
		return strings.TrimSpace(envelope.SenderIDs.OpenID) != "" || strings.TrimSpace(envelope.SenderIDs.UserID) != "" || strings.TrimSpace(envelope.SenderIDs.UnionID) != "", nil
	}
	// App/bot output may only be reconciled as an observation by workingress;
	// it never becomes a human command or evidence of human consent.
	return envelope.SenderType == "app" || envelope.SenderType == "bot", nil
}

type teamPublisherResolver struct{ directory teamDirectory }

func (r teamPublisherResolver) Resolve(ctx context.Context, binding work.RoleBotBinding) (workpublish.Sender, error) {
	gateway, err := r.directory.gateway(ctx, binding)
	if err != nil {
		return nil, err
	}
	return gateway.ProjectChatAPI(), nil
}

type teamResourceResolver struct{ directory teamDirectory }

func (r teamResourceResolver) Resolve(ctx context.Context, binding work.RoleBotBinding) (workservice.ResourceConnector, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	controller, ok := r.directory.app.gateway.(*feishu.MultiGatewayController)
	if !ok {
		return nil, fmt.Errorf("configured multi-application controller required")
	}
	gateway, err := controller.ProjectGateway(binding.GatewayRef, binding.AppID)
	if err != nil {
		return nil, err
	}
	return workservice.NewFeishuResourceConnector(workservice.FeishuConnectorIdentity(binding), gateway)
}
