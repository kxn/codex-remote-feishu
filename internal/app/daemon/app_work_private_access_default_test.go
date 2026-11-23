package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateAccessDefaultDoesNotChangeProjectOrReviewAuthority(t *testing.T) {
	for _, scope := range []work.ScopeKind{work.ScopePersonal, work.ScopeProject, work.ScopeBootstrap} {
		for _, purpose := range []work.ContextPurpose{"", work.PurposeBusiness, work.PurposeControl, work.PurposeReview} {
			for _, legacy := range []string{"", agentproto.AccessModeFullAccess, agentproto.AccessModeConfirm, agentproto.AccessModeAcceptEdits} {
				record := state.BotCapabilitySettingsRecord{PromptOverride: state.ModelConfigRecord{AccessMode: legacy}}
				got := teamPrivateDefaultAccess(work.WorkScope{Kind: scope}, work.WorkContext{Purpose: purpose}, record)
				want := ""
				if scope == work.ScopePersonal && purpose != work.PurposeReview {
					want = agentproto.EffectiveAccessMode(legacy)
				}
				if got != want {
					t.Fatalf("scope=%s purpose=%s legacy=%s access=%s want=%s", scope, purpose, legacy, got, want)
				}
			}
		}
	}
}

func TestPrivatePlanDefaultAndExplicitContextChoice(t *testing.T) {
	for _, kind := range []work.ScopeKind{work.ScopePersonal, work.ScopeProject, work.ScopeBootstrap} {
		for _, purpose := range []work.ContextPurpose{work.PurposeBusiness, work.PurposeControl, work.PurposeReview} {
			for _, explicit := range []bool{true, false} {
				record := state.BotCapabilitySettingsRecord{PlanMode: state.PlanModeSettingOn, PlanModeOverrideSet: explicit}
				want := ""
				if kind == work.ScopePersonal && purpose != work.PurposeReview && explicit {
					want = "on"
				}
				if got := teamPrivateDefaultPlan(work.WorkScope{Kind: kind}, work.WorkContext{Purpose: purpose}, record); got != want {
					t.Fatalf("kind=%s purpose=%s explicit=%v got=%s", kind, purpose, explicit, got)
				}
			}
		}
	}
	snapshot := worksettings.Snapshot{Settings: state.BotCapabilitySettingsRecord{PlanMode: state.PlanModeSettingOn, PlanModeOverrideSet: true}}
	off := "off"
	if got := teamPrivatePlanSummary(snapshot, work.ContextPreferences{PlanMode: &off}); got != "off" {
		t.Fatal(got)
	}
	if got := teamPrivatePlanSummary(snapshot, work.ContextPreferences{}); got != "on（个人默认）" {
		t.Fatal(got)
	}
}
