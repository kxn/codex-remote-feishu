package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestApplyFeishuPermissionVerificationResultClearsGrantedGap(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	if !app.observeFeishuPermissionError("app-1", &feishu.APIError{
		API:  "im.v1.message.create",
		Code: 99990001,
		Msg:  "permission denied",
		PermissionViolations: []feishu.APIErrorPermissionViolation{
			{Type: "tenant", Subject: "drive:drive"},
		},
	}) {
		t.Fatal("expected permission gap to be recorded")
	}

	app.applyFeishuPermissionVerificationResult("app-1", []feishu.AppScopeStatus{
		{ScopeName: "drive:drive", ScopeType: "tenant", GrantStatus: 1},
	}, nil)

	if got := app.snapshotFeishuPermissionGaps("app-1"); len(got) != 0 {
		t.Fatalf("expected granted scope to clear gap, got %#v", got)
	}
}

func TestApplyFeishuPermissionVerificationResultKeepsGapOnVerifyFailure(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	if !app.observeFeishuPermissionError("app-1", &feishu.APIError{
		API:  "im.v1.message.create",
		Code: 99990001,
		Msg:  "permission denied",
		PermissionViolations: []feishu.APIErrorPermissionViolation{
			{Type: "tenant", Subject: "im:message"},
		},
	}) {
		t.Fatal("expected permission gap to be recorded")
	}

	app.applyFeishuPermissionVerificationResult("app-1", nil, errors.New("scope list failed"))

	got := app.snapshotFeishuPermissionGaps("app-1")
	if len(got) != 1 {
		t.Fatalf("expected verification failure to keep gap, got %#v", got)
	}
	if got[0].LastVerified.IsZero() {
		t.Fatalf("expected verify timestamp to be recorded, got %#v", got[0])
	}
}

func TestApplyFeishuPermissionVerificationResultClearsBrokerPermissionBlocks(t *testing.T) {
	gateway := &permissionClearingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	if !app.observeFeishuPermissionError("app-1", &feishu.APIError{
		API:  "im.v1.message.create",
		Code: 99990001,
		Msg:  "permission denied",
		PermissionViolations: []feishu.APIErrorPermissionViolation{
			{Type: "tenant", Subject: "im:message"},
		},
	}) {
		t.Fatal("expected permission gap to be recorded")
	}
	app.applyFeishuPermissionVerificationResult("app-1", []feishu.AppScopeStatus{
		{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
	}, nil)
	if len(gateway.clearCalls) != 1 {
		t.Fatalf("expected permission block clear to be forwarded once, got %#v", gateway.clearCalls)
	}
	if gateway.clearCalls[0].gatewayID != "app-1" {
		t.Fatalf("unexpected gateway id: %#v", gateway.clearCalls[0])
	}
	if len(gateway.clearCalls[0].scopes) != 1 || gateway.clearCalls[0].scopes[0].ScopeName != "im:message" {
		t.Fatalf("unexpected forwarded scopes: %#v", gateway.clearCalls[0].scopes)
	}
}

func TestPrimaryPermissionDecisionAcceptsGroupMessageScopes(t *testing.T) {
	for _, scope := range []string{"im:message.group_msg", "im:message.group_msg:readonly"} {
		decision := primaryPermissionDecisionFromScopes([]feishu.AppScopeStatus{
			{ScopeName: scope, ScopeType: "tenant", GrantStatus: 1},
		}, nil)
		if !decision.Allowed || decision.Scope != scope {
			t.Fatalf("scope %s decision = %#v, want allowed", scope, decision)
		}
	}
}

func TestPrimaryPermissionDecisionRejectsUserGroupMessageScope(t *testing.T) {
	decision := primaryPermissionDecisionFromScopes([]feishu.AppScopeStatus{
		{ScopeName: "im:message.group_msg", ScopeType: "user", GrantStatus: 1},
	}, nil)
	if decision.Allowed || decision.Reason != "missing_group_message_scope" {
		t.Fatalf("user-token scope decision = %#v, want missing tenant scope", decision)
	}
}

func TestPrimaryPermissionDecisionRejectsMissingScopeAndErrors(t *testing.T) {
	if decision := primaryPermissionDecisionFromScopes([]feishu.AppScopeStatus{
		{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
	}, nil); decision.Allowed || decision.Reason != "missing_group_message_scope" {
		t.Fatalf("missing scope decision = %#v, want missing", decision)
	}
	if decision := primaryPermissionDecisionFromScopes(nil, errors.New("boom")); decision.Allowed || decision.Reason != "scope_read_failed" || decision.Err == nil {
		t.Fatalf("error decision = %#v, want failed with err", decision)
	}
}

func TestPrimaryPermissionCheckerUsesFreshCacheWhenNotForced(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	now := time.Now().UTC()
	app.storePrimaryBotPermissionCache("app-1", orchestrator.PrimaryBotPermissionDecision{
		Allowed: true,
		Scope:   "im:message.group_msg",
	}, now, true)

	decision := app.CheckPrimaryBotPermission(context.Background(), orchestrator.PrimaryBotPermissionRequest{
		GatewayID: "app-1",
	})
	if !decision.Allowed || decision.Scope != "im:message.group_msg" {
		t.Fatalf("cached decision = %#v, want allowed group_msg", decision)
	}
}

func serverIdentityForTest() agentproto.ServerIdentity {
	return agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
	}
}

type permissionClearingGateway struct {
	recordingGateway
	clearCalls []permissionClearCall
}

type permissionClearCall struct {
	gatewayID string
	scopes    []feishu.AppScopeStatus
}

func (g *permissionClearingGateway) ClearGrantedPermissionBlocks(gatewayID string, scopes []feishu.AppScopeStatus) {
	g.clearCalls = append(g.clearCalls, permissionClearCall{
		gatewayID: gatewayID,
		scopes:    append([]feishu.AppScopeStatus(nil), scopes...),
	})
}
