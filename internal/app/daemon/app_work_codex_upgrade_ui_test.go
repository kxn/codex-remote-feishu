package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

type uiCodexUpgrader struct {
	store    *workstore.Store
	requests []workupgrade.Request
	records  map[string]workupgrade.Record
}

func (u *uiCodexUpgrader) Request(ctx context.Context, request workupgrade.Request) (workupgrade.Record, error) {
	err := u.store.Within(ctx, func(tx *workstore.Tx) error { _, _, e := tx.ValidatePrivateAction(request.Source); return e })
	if err != nil {
		return workupgrade.Record{}, err
	}
	u.requests = append(u.requests, request)
	if record, ok := u.records[request.ID]; ok {
		return record, nil
	}
	record := workupgrade.Record{ID: request.ID, HostID: "local", Source: request.Source, TargetVersion: request.TargetVersion, InstallationFingerprint: request.InstallationFingerprint, Stage: "draining", Revision: 1}
	u.records[record.ID] = record
	return record, nil
}
func (u *uiCodexUpgrader) Status(_ context.Context, id string) (workupgrade.Record, error) {
	record, ok := u.records[id]
	if !ok {
		return record, workstore.ErrNotFound
	}
	return record, nil
}
func (*uiCodexUpgrader) Run(context.Context, int) error { return nil }

func codexUpgradeUIFixture(t *testing.T) (*App, teamHumanSource, *uiCodexUpgrader, string) {
	t.Helper()
	a, source := teamPickerFixture(t)
	u := &uiCodexUpgrader{store: a.teamRuntime.store, records: map[string]workupgrade.Record{}}
	a.teamRuntime.codexUpgrader = u
	a.teamRuntime.codexUpgradeHostID = "local"
	a.teamRuntime.active.Store(true)
	a.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.DefaultAppConfig()}, nil
	}
	a.codexUpgradeRuntime.Inspect = func(context.Context, codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
		return codexupgrade.Installation{SourceKind: codexupgrade.SourceStandaloneNPM, ConfiguredBinary: "codex", PackageVersion: "1.0.0"}, nil
	}
	a.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) { return "2.0.0", nil }
	id, err := a.queueTeamCodexUpgrade(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	return a, source, u, id
}

func TestCodexUpgradeUIConfirmsFrozenVersionAndActualHumanSource(t *testing.T) {
	a, source, u, id := codexUpgradeUIFixture(t)
	if len(u.requests) != 0 {
		t.Fatal("opening the version card started an upgrade")
	}
	view, click := pickerAction(t, a, source, id, "host.codex-upgrade.confirm")
	if err := a.handleTeamCodexUpgradeView(t.Context(), click, view, "host.codex-upgrade.confirm"); err != nil {
		t.Fatal(err)
	}
	if len(u.requests) != 1 || u.requests[0].TargetVersion != "2.0.0" || u.requests[0].Source.InteractionID != click.ID || u.requests[0].Source.PersonalScopeID != source.ScopeID || u.requests[0].ID != teamID("codex-upgrade", view.TargetID) {
		t.Fatalf("wrong admitted maintenance request: %+v", u.requests)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		body, e := tx.GetContent(source.ScopeID, view.TargetID)
		if e != nil {
			return e
		}
		var candidate teamCodexUpgradeCandidate
		if e = json.Unmarshal(body.Data, &candidate); e != nil {
			return e
		}
		if candidate.InstallationFingerprint == "" || candidate.InstallationFingerprint != u.requests[0].InstallationFingerprint {
			t.Fatal("confirmation did not freeze actual installation")
		}
		fresh, e := tx.GetUIView(id)
		if e != nil {
			return e
		}
		if !fresh.Invalidated {
			t.Fatal("completed confirmation remains actionable")
		}
		interaction, e := tx.GetHumanInteraction(click.ID)
		if e != nil {
			return e
		}
		if interaction.State != "handled" {
			t.Fatal("confirmed maintenance never settled the callback")
		}
		return nil
	})
}

func TestCodexUpgradeUICancelCannotDenyAnAlreadyRecordedInstall(t *testing.T) {
	a, source, u, id := codexUpgradeUIFixture(t)
	view, click := pickerAction(t, a, source, id, "host.codex-upgrade.cancel")
	upgradeID := teamID("codex-upgrade", view.TargetID)
	u.records[upgradeID] = workupgrade.Record{ID: upgradeID, Source: source.Authority, Stage: "installing_global", TargetVersion: "2.0.0", Revision: 3}
	if err := a.handleTeamCodexUpgradeView(t.Context(), click, view, "host.codex-upgrade.cancel"); err != nil {
		t.Fatal(err)
	}
	if len(u.requests) != 0 {
		t.Fatal("cancel started another installer")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews(source.ScopeID)
		if e != nil {
			return e
		}
		found := false
		for _, v := range views {
			if v.Kind == "host.codex-upgrade.status" && v.TargetID == upgradeID {
				found = true
			}
			if v.Kind == "host.result" {
				t.Fatal("reported cancellation after install was persisted")
			}
		}
		if !found {
			t.Fatal("existing maintenance progress was not returned")
		}
		return nil
	})
}

func TestCodexUpgradeCompletionNeedsNoModelAndRecoversMissingInitialStatus(t *testing.T) {
	a, source, u, id := codexUpgradeUIFixture(t)
	var original workstore.UIView
	pickerTx(t, a, func(tx *workstore.Tx) error { var e error; original, e = tx.GetUIView(id); return e })
	upgradeID := teamID("codex-upgrade", original.TargetID)
	u.records[upgradeID] = workupgrade.Record{ID: upgradeID, Source: source.Authority, Stage: "completed", TargetVersion: "2.0.0", ManagedVersion: "2.0.0", Revision: 7}
	for n := 0; n < 2; n++ {
		if err := a.runTeamCodexUpgradeViews(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		views, e := tx.ListUIViews(source.ScopeID)
		if e != nil {
			return e
		}
		count := 0
		for _, view := range views {
			if view.Kind == "host.codex-upgrade.result" {
				count++
				if view.ChatID != source.ChatID || view.Binding.AppID != source.Gateway.AppID || view.PrincipalID != source.PrincipalID {
					t.Fatal("completion guessed a destination")
				}
				body, e := tx.GetContent(view.ScopeID, view.BodyRef)
				if e != nil {
					return e
				}
				if !strings.Contains(string(body.Data), "更新已完成") {
					t.Fatal("completion has no actual status")
				}
			}
		}
		if count != 1 {
			t.Fatalf("completion notice count %d", count)
		}
		return nil
	})
}
