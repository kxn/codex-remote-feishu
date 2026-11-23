package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) receiveTeamPlatformEvent(ctx context.Context, event feishu.PlatformIngressEvent) error {
	gateway, err := (teamDirectory{app: a, store: a.teamRuntime.store}).ResolveGateway(ctx, event.GatewayRef, event.AppID)
	if err != nil {
		return err
	}
	facts := event
	facts.ReceivedAt = time.Time{}
	raw, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		id, replay, e := tx.AcceptPlatformObservation(workstore.PlatformObservation{Gateway: gateway, EventID: event.EventID, Tenant: event.Tenant, ChatID: event.ChatID, MessageID: event.MessageID, Kind: event.Kind, Facts: raw, ReceivedAt: event.ReceivedAt})
		if e == nil && !replay && event.Kind == "reaction.created" {
			return tx.RegisterPrivateReactionAction(id)
		}
		if e != nil || replay || (event.Kind != "bot.removed" && event.Kind != "chat.deleted") {
			return e
		}
		channel, e := tx.GetProjectChannelBinding(event.Tenant, event.ChatID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if !channel.Active {
			return nil
		}
		binding, e := tx.GetRoleBotBinding(gateway.RoleID)
		if e != nil {
			return e
		}
		return tx.ApplyProjectMembershipObservation(workstore.ProjectMembershipObservation{
			ProjectID: channel.ProjectID, Binding: binding, ChannelGeneration: channel.BindingGeneration,
			Tenant: event.Tenant, ChatID: event.ChatID, ChatDeleted: event.Kind == "chat.deleted",
			Source: work.SourceRef{Kind: "platform_observation", ID: id, Version: "1", EvidenceRef: id},
		})
	})
	if err == nil && event.Kind == "reaction.created" {
		a.teamRuntime.wakeLane("private-reactions")
	}
	return err
}
