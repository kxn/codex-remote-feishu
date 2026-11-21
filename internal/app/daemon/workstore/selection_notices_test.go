package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestSelectionNoticePersistsBeforeSendAndUnknownCannotAutoRepeat(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	now := time.Now().UTC()
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "r", BotGeneration: 1}
	var id string
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		record, _, err := tx.AcceptRawIngress(work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "unbound-chat", ChatType: "group", MessageID: "message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"choose team"}`, ReceivedAt: now}, gateway)
		id = record.ID
		return err
	})
	input := BeginSelectionNoticeRequest{IngressID: id, Gateway: gateway, Text: "Please choose a configured leader", CandidateRoleIDs: []string{"r", "expert"}, Now: now}
	requireTx(t, s, func(tx *Tx) error {
		notice, send, err := tx.TryBeginSelectionNotice(input)
		if err != nil {
			return err
		}
		if !send || notice.State != work.OperationSending || notice.ChatID != "unbound-chat" || notice.UUID == "" {
			t.Fatal("selection send intent was not persisted with real destination")
		}
		if err = tx.FinishSelectionNotice(id, 1, SelectionNoticeOutcome{State: work.OperationUnknown, LastError: "response lost", Now: now}); err != nil {
			return err
		}
		again, send, err := tx.TryBeginSelectionNotice(input)
		if err != nil {
			return err
		}
		if send || again.UUID != notice.UUID || again.State != work.OperationUnknown {
			t.Fatal("uncertain selection notice automatically resent")
		}
		return nil
	})
}
func TestSelectionNoticeCannotChangeReceivingApplicationOrChat(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "r", BotGeneration: 1}
	now := time.Now().UTC()
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		record, _, err := tx.AcceptRawIngress(work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "unbound-chat", ChatType: "group", MessageID: "message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"choose team"}`, ReceivedAt: now}, gateway)
		if err != nil {
			return err
		}
		input := BeginSelectionNoticeRequest{IngressID: record.ID, Gateway: gateway, Text: "Choose", Now: now}
		wrong := input
		wrong.Gateway.AppID = "another-app"
		if _, _, err = tx.TryBeginSelectionNotice(wrong); err == nil {
			t.Fatal("selection notice escaped original SDK app")
		}
		if _, _, err = tx.TryBeginSelectionNotice(input); err != nil {
			return err
		}
		if err = tx.FinishSelectionNotice(record.ID, 1, SelectionNoticeOutcome{State: work.OperationConfirmed, ExternalMessageID: "external", ChatID: "different-chat", Now: now}); err == nil {
			t.Fatal("receipt from another chat confirmed selection delivery")
		}
		return nil
	})
}
