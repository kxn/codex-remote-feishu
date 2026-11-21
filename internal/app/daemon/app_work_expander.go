package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// teamMessageExpander never consults a legacy Surface or a default Bot.
// The accepted SDK identity must still match a current configured binding.
type teamMessageExpander struct{ directory teamDirectory }

func (e teamMessageExpander) Expand(ctx context.Context, raw work.RawIngressEnvelope) (workingress.ExpandedMessage, error) {
	return workingress.ExpandedMessage{}, fmt.Errorf("accepted ingress identity required")
}
func (e teamMessageExpander) ExpandAccepted(ctx context.Context, record work.IngressRecord) (workingress.ExpandedMessage, error) {
	raw := record.Envelope
	gateway, err := e.directory.ResolveGateway(ctx, raw.GatewayRef, raw.AppID)
	if err != nil {
		return workingress.ExpandedMessage{}, err
	}
	var binding work.RoleBotBinding
	err = e.directory.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		binding, err = tx.GetRoleBotBinding(gateway.RoleID)
		return err
	})
	if err != nil {
		return workingress.ExpandedMessage{}, err
	}
	if binding.GatewayRef != raw.GatewayRef || binding.AppID != raw.AppID || binding.IdentityGeneration != gateway.BotGeneration || binding.IdentityGeneration != record.Gateway.BotGeneration || binding.RoleID != record.Gateway.RoleID {
		return workingress.ExpandedMessage{}, fmt.Errorf("attachment receiving identity changed")
	}
	live, err := e.directory.gateway(ctx, binding)
	if err != nil {
		return workingress.ExpandedMessage{}, err
	}
	expanded, err := live.ExpandTeamMessage(ctx, raw)
	if err != nil {
		return workingress.ExpandedMessage{}, err
	}
	result := workingress.ExpandedMessage{Text: expanded.Text, Complete: expanded.Complete, Issues: expanded.Issues}
	for _, asset := range expanded.Attachments {
		result.Attachments = append(result.Attachments, workingress.ExpandedAttachment{Kind: asset.Kind, MediaType: asset.MediaType, FileName: asset.FileName, SourceMessageID: asset.SourceMessageID, Data: asset.Data})
	}
	return result, nil
}
