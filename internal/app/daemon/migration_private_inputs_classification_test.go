package daemon

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestClaimedControlInputDoesNotBecomeRecoverableHumanText(t *testing.T) {
	row := func(key string, v any) workstore.PersonalExportRecord {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return workstore.PersonalExportRecord{Key: key, Payload: b}
	}
	owner := privatechat.Owner{TenantID: "tenant", AppID: "app", GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "human"}
	human := work.Message{ID: "human-message", ScopeID: "scope", Source: work.Actor{Kind: work.ActorPrincipal, ID: "principal"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: "sdk-human"}}
	control := work.Inbox{ID: "control", ScopeID: "scope", RecipientContextID: "control-context", MessageID: "human-message", State: work.InboxClaimed, ControlOfInputID: "old-input"}
	ordinary := work.Inbox{ID: "ordinary", ScopeID: "scope", RecipientContextID: "front", MessageID: "human-message", State: work.InboxClaimed}
	rows := map[string][]workstore.PersonalExportRecord{"scopes": {row("scope", work.WorkScope{ID: "scope", PrincipalID: "principal"})}, "messages": {row("human-message", human)}, "inbox": {row("control", control), row("ordinary", ordinary)}}
	report := PrivateMigrationReport{}
	checkpoint := orchestrator.PrivateCheckpoint{}
	if err := mapPersonalMigrationInputs(&report, &checkpoint, rows, nil, work.PrivateNavigation{Mode: work.PrivatePersonal, FrontContextID: "front"}, owner, nil); err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, h := range report.InputHolds {
		reasons[h.InboxID] = h.Reason
	}
	if reasons["control"] != "internal_component_nonreplayable" || reasons["ordinary"] != "claimed_input_requires_native_reconciliation" {
		t.Fatalf("wrong classifications: %+v", reasons)
	}
}
