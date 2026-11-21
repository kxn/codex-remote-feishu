package daemon

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"testing"
)

func TestTeamAuthorityFencesLegacyCodexBinaryUpgradeBeforeAnyEffects(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "cutover-fenced", true: "active"}[active], func(t *testing.T) {
			a := &App{teamRuntime: &teamRuntime{}}
			a.teamRuntime.active.Store(active)
			a.teamRuntime.legacyFenced.Store(!active)
			a.codexUpgradeRuntime.Install = func(context.Context, codexupgrade.Installation, string) error {
				t.Fatal("legacy native install ran under team authority")
				return nil
			}
			// No old orchestrator or installation resolver is present: the authority
			// boundary must reject before consulting or restarting legacy instances.
			if err := a.startStandaloneCodexUpgrade(t.Context(), codexUpgradeStartRequest{TargetVersion: "new"}); err == nil {
				t.Fatal("legacy upgrade bypassed new runtime")
			}
		})
	}
}
