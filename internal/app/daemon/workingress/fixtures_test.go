package workingress

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"testing"
	"time"
)

var ingressNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

type testDirectory struct {
	gateways  map[string]work.IngressGateway
	leader    string
	leaderErr error
}

func (d *testDirectory) ResolveGateway(_ context.Context, gateway, app string) (work.IngressGateway, error) {
	v, ok := d.gateways[gateway]
	if !ok || v.AppID != app {
		return v, fmt.Errorf("unknown actual gateway")
	}
	return v, nil
}
func (d *testDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return d.leader, d.leaderErr
}
func (d *testDirectory) ResolveMention(_ context.Context, _ work.IngressGateway, mention work.RawMention) (string, error) {
	for _, v := range d.gateways {
		if v.BotOpenID == mention.IDs.OpenID {
			return v.RoleID, nil
		}
	}
	return "", nil
}
func txRun(t *testing.T, s *workstore.Store, fn func(*workstore.Tx) error) {
	t.Helper()
	if err := s.Within(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}
func ingressFixture(t *testing.T, project bool) (*Ingress, *workstore.Store, *testDirectory) {
	t.Helper()
	s, err := workstore.Open(t.Context(), filepath.Join(t.TempDir(), "work.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	directory := &testDirectory{leader: "leader", gateways: map[string]work.IngressGateway{}}
	txRun(t, s, func(tx *workstore.Tx) error {
		for _, role := range []string{"leader", "expert"} {
			gateway := work.IngressGateway{GatewayRef: "gateway:" + role, AppID: "app:" + role, RoleID: role, BotGeneration: 1, BotOpenID: "bot:" + role}
			directory.gateways[gateway.GatewayRef] = gateway
			if err := tx.PutRole(work.Role{ID: role, Name: role, Description: "Responsibility", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: role, GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, IdentityGeneration: 1}, 0); err != nil {
				return err
			}
		}
		if err := tx.PutTeam(work.Team{ID: "team", Name: "Team", LeaderRoleID: "leader", Revision: 1}, 0); err != nil {
			return err
		}
		for _, role := range []string{"leader", "expert"} {
			if err := tx.PutTeamMember(work.TeamMember{TeamID: "team", RoleID: role, Active: true, Revision: 1}, 0); err != nil {
				return err
			}
		}
		if project {
			return seedProject(tx, "scope", "project", "group", 0)
		}
		return nil
	})
	ingress, err := New(s, Config{Directory: directory, Now: func() time.Time { return ingressNow }})
	if err != nil {
		t.Fatal(err)
	}
	return ingress, s, directory
}
func seedProject(tx *workstore.Tx, scope, id, chat string, expected int64) error {
	if err := tx.PutScope(work.WorkScope{ID: scope, Kind: work.ScopeProject, TeamID: "team", ProjectID: id, Revision: 1}, 0); err != nil {
		return err
	}
	if err := tx.PutProject(work.Project{ID: id, ScopeID: scope, TeamID: "team", Name: id, LeaderRoleID: "leader", State: work.ProjectActive, Revision: 1}, 0); err != nil {
		return err
	}
	for _, role := range []string{"leader", "expert"} {
		if err := tx.PutProjectMember(work.ProjectMember{ProjectID: id, RoleID: role, State: work.ProjectMemberReady, Revision: 1}, 0); err != nil {
			return err
		}
	}
	return tx.PutProjectChannelBinding(work.ProjectChannelBinding{ProjectID: id, Tenant: "tenant", ChatID: chat, Purpose: "main", BindingGeneration: expected + 1, Active: true}, expected)
}
func rawMessage(role, id, chat, chatType, openID, userID, text string) work.RawIngressEnvelope {
	return work.RawIngressEnvelope{GatewayRef: "gateway:" + role, AppID: "app:" + role, EventID: "event:" + role + ":" + id, Tenant: "tenant", ChatID: chat, ChatType: chatType, MessageID: id, SenderType: "user", SenderIDs: work.SenderIDs{OpenID: openID, UserID: userID}, MessageType: "text", RawContent: fmt.Sprintf(`{"text":%q}`, text), ReceivedAt: ingressNow}
}
func receive(t *testing.T, i *Ingress, env work.RawIngressEnvelope) ReceiveReceipt {
	t.Helper()
	receipt, err := i.Receive(t.Context(), env)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}
func process(t *testing.T, i *Ingress) ProcessResult {
	t.Helper()
	result, err := i.ProcessPending(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) > 0 {
		t.Fatalf("processing failures %+v", result.Failures)
	}
	return result
}
