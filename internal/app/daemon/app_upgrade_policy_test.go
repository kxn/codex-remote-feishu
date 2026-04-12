package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/buildinfo"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func withBuildFlavorForDaemonTest(t *testing.T, flavor buildinfo.Flavor) {
	t.Helper()
	previous := buildinfo.FlavorValue
	buildinfo.FlavorValue = string(flavor)
	t.Cleanup(func() {
		buildinfo.FlavorValue = previous
	})
}

func TestBuildUpgradeStatusCatalogHidesShippingOnlyOptions(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorShipping)

	catalog := buildUpgradeStatusCatalog(install.InstallState{
		CurrentTrack:   install.ReleaseTrackProduction,
		CurrentVersion: "v1.0.0",
	}, false)
	if strings.Contains(catalog.Summary, "本地升级产物：") {
		t.Fatalf("shipping catalog should hide local upgrade artifact path, got %#v", catalog.Summary)
	}
	if got := len(catalog.Sections[0].Entries[0].Buttons); got != 2 {
		t.Fatalf("shipping quick buttons = %d, want 2", got)
	}
	if got := len(catalog.Sections[1].Entries[0].Buttons); got != 2 {
		t.Fatalf("shipping track buttons = %d, want 2", got)
	}
	for _, button := range catalog.Sections[1].Entries[0].Buttons {
		if strings.Contains(button.CommandText, "alpha") {
			t.Fatalf("shipping catalog should hide alpha track button: %#v", catalog.Sections[1].Entries[0].Buttons)
		}
	}
}

func TestUpgradeTrackRejectsDisallowedShippingTrack(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorShipping)

	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade track alpha",
	})

	waitForUpgradeNoticeBody(t, gateway, "当前构建不支持 alpha track")
	assertUpgradeStateTrack(t, statePath, install.ReleaseTrackProduction)
}

func TestUpgradeLocalRejectedInShippingFlavor(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorShipping)

	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade local",
	})

	waitForUpgradeNoticeBody(t, gateway, "当前构建不支持 `/upgrade local`")
}

func waitForUpgradeNoticeBody(t *testing.T, gateway *lifecycleGateway, needle string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			if op.CardTitle == "Upgrade" && strings.Contains(op.CardBody, needle) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for upgrade notice containing %q", needle)
}

func assertUpgradeStateTrack(t *testing.T, statePath string, want install.ReleaseTrack) {
	t.Helper()
	stateValue, err := install.LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if stateValue.CurrentTrack != want {
		t.Fatalf("CurrentTrack = %q, want %q", stateValue.CurrentTrack, want)
	}
}
