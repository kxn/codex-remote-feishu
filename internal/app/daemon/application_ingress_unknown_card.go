package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// recoverUnknownPrivateCard treats the callback as a candidate message ID
// only. The exact live Bot reads that message and proves sender, chat and the
// whole previously frozen card, including its already-issued random token.
func (g *applicationIngress) recoverUnknownPrivateCard(ctx context.Context, event ingressowner.Event) error {
	if event.Kind != ingressowner.Card || event.CardKind != "private_view" || event.CardRef == "" || event.MessageID == "" || g.host == nil || g.host.teamRuntime == nil || g.host.applications == nil || g.host.applications.publisher == nil || g.host.applications.publisher.ownership == nil {
		return ingressowner.ErrUnresolved
	}
	issued, err := g.router.PrivateViewByToken(ctx, event.CardRef)
	if err != nil {
		return err
	}
	directory := teamDirectory{app: g.host, store: g.host.teamRuntime.store}
	identity, err := directory.ResolveGateway(ctx, event.Source.GatewayID, event.Key.AppID)
	if err != nil {
		return err
	}
	if identity.BotGeneration != event.Source.BindingGeneration {
		return ingressowner.ErrConflict
	}
	live, err := directory.gateway(ctx, work.RoleBotBinding{RoleID: identity.RoleID, GatewayRef: identity.GatewayRef, AppID: identity.AppID, IdentityGeneration: identity.BotGeneration})
	if err != nil {
		return err
	}
	actual, err := live.ProjectChatAPI().ReadPublishedMessage(ctx, event.MessageID)
	if err != nil {
		return err
	}
	chatID := event.ChatID
	if chatID == "" {
		chatID = actual.ChatID
	}
	b, err := g.router.PrivateBindingForOwner(ctx, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, chatID, issued.Owner)
	if err != nil {
		return err
	}
	if event.Operator != b.Operator {
		return ingressowner.ErrConflict
	}
	owner := privatechat.Owner{GatewayID: b.GatewayID, SurfaceID: gatewaypkg.SurfaceIDForInbound(b.GatewayID, "", "p2p", b.Operator.ID), ChatID: b.ChatID, UserID: b.Operator.ID, TenantID: b.Tenant, AppID: b.AppID}
	publication, err := g.host.applications.journal.LoadPublication(ctx, owner, b.Owner.Epoch, issued.EffectID)
	if err != nil {
		return err
	}
	if issued.Ordinal < 0 || issued.Ordinal >= len(publication.Operations) {
		return ingressowner.ErrConflict
	}
	item := publication.Operations[issued.Ordinal]
	if item.Ordinal != issued.Ordinal {
		return ingressowner.ErrConflict
	}
	if item.State == "confirmed" {
		return g.confirmCallbackPublication(ctx, owner, b.Owner.Epoch, issued.EffectID, issued.Ordinal)
	}
	if item.State != "unknown" {
		return ingressowner.ErrUnresolved
	}
	op, err := feishu.ThawPrivateOperation(item.Body)
	if err != nil {
		return err
	}
	if (op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard) || op.ChatID != b.ChatID || op.GatewayID != b.GatewayID || op.SurfaceSessionID != owner.SurfaceID || op.MessageID != "" && op.MessageID != event.MessageID {
		return ingressowner.ErrConflict
	}
	rawCard, err := feishu.PrivateOperationCard(item.Body)
	if err != nil {
		return err
	}
	unstamped, token, err := feishu.UnstampPrivateView(item.Body)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(unstamped)
	if token != issued.Token || hex.EncodeToString(digest[:]) != issued.RenderHash {
		return ingressowner.ErrConflict
	}
	if err = verifyObservedPrivateCard(rawCard, actual, identity.AppID, b.ChatID, event.MessageID); err != nil {
		return err
	}
	op.MessageID = event.MessageID
	receipt, err := feishu.PrivateReceiptForOperation(op, time.Now().UTC())
	if err != nil {
		return err
	}
	rawReceipt, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	verifier := privatePublicationResult{o: owner, generation: b.Owner.Epoch, id: issued.EffectID, ordinal: issued.Ordinal, body: item.Body, receipt: rawReceipt}
	if err = g.host.applications.journal.CompletePublicationOperation(ctx, owner, b.Owner.Epoch, issued.EffectID, issued.Ordinal, "confirmed", rawReceipt, verifier); err != nil {
		return err
	}
	if err = g.confirmCallbackPublication(ctx, owner, b.Owner.Epoch, issued.EffectID, issued.Ordinal); err != nil {
		return err
	}
	return nil
}

func verifyObservedPrivateCard(rawCard json.RawMessage, actual feishu.ObservedPublication, appID, chatID, messageID string) error {
	var expectedCard, actualCard any
	if actual.Deleted || actual.MessageID != messageID || actual.ChatID != chatID || actual.SenderID != appID || actual.SenderIDType != "app_id" || actual.SenderType != "app" || actual.MessageType != "interactive" || json.Unmarshal(rawCard, &expectedCard) != nil || json.Unmarshal([]byte(actual.Content), &actualCard) != nil || !reflect.DeepEqual(expectedCard, actualCard) {
		return fmt.Errorf("platform card does not match frozen publication")
	}
	return nil
}
