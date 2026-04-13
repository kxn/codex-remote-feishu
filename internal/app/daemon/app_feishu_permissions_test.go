package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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

func serverIdentityForTest() agentproto.ServerIdentity {
	return agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC),
	}
}
