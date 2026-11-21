package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestDelayedSDKMessageCannotReviveLaterRevokedProjectAccess(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	requireTx(t, s, func(tx *Tx) error {
		gateway := work.IngressGateway{GatewayRef: "g", AppID: "app", RoleID: "r", BotGeneration: 1}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "g", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
			return e
		}
		raw := work.RawIngressEnvelope{GatewayRef: "g", AppID: "app", EventID: "initial", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "initial", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "open"}, MessageType: "text", RawContent: `{"text":"hello"}`, MessageCreatedAt: now, ReceivedAt: now}
		record, _, e := tx.AcceptRawIngress(raw, gateway)
		if e != nil {
			return e
		}
		evidence := work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: record.ID}
		principal, _, e := tx.ResolvePrincipal(evidence)
		if e != nil {
			return e
		}
		if e = tx.GrantProjectAccessFromIngress("s", principal.ID, record.ID, now.Add(time.Hour)); e != nil {
			return e
		}
		access, e := tx.GetProjectPrincipalAccess("s", principal.ID)
		if e != nil {
			return e
		}
		access.State = work.ProjectAccessRevoked
		access.VerifiedAt = now.Add(10 * time.Minute)
		access.ExpiresAt = now.Add(time.Hour)
		access.Revision++
		if e = tx.putProjectPrincipalAccess(access, 1); e != nil {
			return e
		}
		raw.EventID = "delayed"
		raw.MessageID = "delayed"
		raw.MessageCreatedAt = now.Add(time.Minute)
		raw.ReceivedAt = now.Add(20 * time.Minute)
		record, _, e = tx.AcceptRawIngress(raw, gateway)
		if e != nil {
			return e
		}
		if e = tx.GrantProjectAccessFromIngress("s", principal.ID, record.ID, raw.MessageCreatedAt.Add(time.Hour)); e != nil {
			return e
		}
		after, e := tx.GetProjectPrincipalAccess("s", principal.ID)
		if e != nil {
			return e
		}
		if after.State != work.ProjectAccessRevoked || after.Revision != access.Revision {
			t.Fatal("late callback revived revoked membership")
		}
		return nil
	})
}
