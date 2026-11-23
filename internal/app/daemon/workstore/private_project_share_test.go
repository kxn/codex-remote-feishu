package workstore

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateShareRequiresCurrentMembershipAndExactIdempotency(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	now := time.Now().UTC()
	var principal work.Principal
	requireTx(t, s, func(tx *Tx) error {
		g := work.IngressGateway{GatewayRef: "g", AppID: "app", RoleID: "r", BotGeneration: 1}
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "g", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		member, e := tx.GetProjectMember("project", "r")
		if e != nil {
			return e
		}
		member.BotGeneration = 1
		member.Revision++
		if e = tx.PutProjectMember(member, 1); e != nil {
			return e
		}
		if e := tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: "project", Tenant: "tenant", ChatID: "group", Purpose: "main", BindingGeneration: 1, Active: true}, 0); e != nil {
			return e
		}
		raw := work.RawIngressEnvelope{GatewayRef: "g", AppID: "app", EventID: "group-event", Tenant: "tenant", ChatID: "group", ChatType: "group", MessageID: "group-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: now}
		record, _, e := tx.AcceptRawIngress(raw, g)
		if e != nil {
			return e
		}
		principal, _, e = tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: record.ID})
		if e != nil {
			return e
		}
		return tx.GrantProjectAccessFromIngress("s", principal.ID, record.ID, now.Add(time.Hour))
	})
	share := PrivateProjectShare{ID: "share", ProjectID: "project", PrincipalID: principal.ID, SourceRoleID: "r", SourceBotGeneration: 1, PrivateOwnerID: "owner", PrivateEpoch: 1, DraftHash: "draft", ConsentEventID: "consent", ConsentFactHash: "fact", Text: "only this", CreatedAt: now}
	requireTx(t, s, func(tx *Tx) error {
		first, e := tx.AdmitPrivateProjectShare(share)
		if e != nil {
			return e
		}
		second, e := tx.AdmitPrivateProjectShare(share)
		if e != nil {
			return e
		}
		if first != second {
			t.Fatal("replay changed receipt")
		}
		msg, e := tx.GetMessage(first.MessageID)
		if e != nil {
			return e
		}
		in, e := tx.GetInbox(first.InboxID)
		if e != nil {
			return e
		}
		scope, e := tx.GetScope("s")
		if e != nil {
			return e
		}
		if _, e = tx.authorizeProjectMessageSource(scope, in, msg, now); e != nil {
			return e
		}
		var original []byte
		if e = tx.tx.QueryRowContext(tx.ctx, `SELECT payload FROM private_project_shares WHERE id=?`, share.ID).Scan(&original); e != nil {
			return e
		}
		if _, e = tx.tx.ExecContext(tx.ctx, `UPDATE private_project_shares SET payload=? WHERE id=?`, append(original, ' '), share.ID); e != nil {
			return e
		}
		if _, e = tx.authorizeProjectMessageSource(scope, in, msg, now); e == nil {
			t.Fatal("tampered share provenance authorized")
		}
		if _, e = tx.tx.ExecContext(tx.ctx, `UPDATE private_project_shares SET payload=? WHERE id=?`, original, share.ID); e != nil {
			return e
		}
		changed := share
		changed.Text = "other"
		if _, e = tx.AdmitPrivateProjectShare(changed); e == nil {
			t.Fatal("same share key changed content")
		}
		access, e := tx.GetProjectPrincipalAccess("s", principal.ID)
		if e != nil {
			return e
		}
		access.VerifiedAt = now.Add(-2 * time.Hour)
		access.ExpiresAt = now.Add(-time.Hour)
		access.Revision++
		if e = tx.putProjectPrincipalAccess(access, 1); e != nil {
			return e
		}
		expired := share
		expired.ID = "expired-share"
		expired.CreatedAt = now.Add(-90 * time.Minute)
		if _, e = tx.AdmitPrivateProjectShare(expired); e == nil {
			t.Fatal("expired access accepted using old card time")
		}
		access.State = work.ProjectAccessRevoked
		access.Revision++
		if e = tx.putProjectPrincipalAccess(access, 2); e != nil {
			return e
		}
		if _, e = tx.authorizeProjectMessageSource(scope, in, msg, now); e == nil {
			t.Fatal("revoked project access delivered")
		}
		access.State = work.ProjectAccessActive
		access.VerifiedAt = now
		access.ExpiresAt = now.Add(time.Hour)
		access.Revision++
		if e = tx.putProjectPrincipalAccess(access, 3); e != nil {
			return e
		}
		member, e := tx.GetProjectMember("project", "r")
		if e != nil {
			return e
		}
		member.BotGeneration = 2
		member.Revision++
		if e = tx.PutProjectMember(member, 2); e != nil {
			return e
		}
		if _, e = tx.authorizeProjectMessageSource(scope, in, msg, now); e == nil {
			t.Fatal("rebound bot membership authorized old share")
		}
		return nil
	})
}
