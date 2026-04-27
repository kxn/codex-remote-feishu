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

	catalog := buildUpgradeRootPageView(install.InstallState{
		CurrentTrack:   install.ReleaseTrackProduction,
		CurrentVersion: "v1.0.0",
	}, false, "", "", "")
	assertCatalogUsesPlainTextContracts(t, &catalog)
	summary := catalogSummaryText(&catalog)
	if got := len(catalog.Sections[0].Entries[0].Buttons); got != 2 {
		t.Fatalf("shipping quick buttons = %d, want 2", got)
	}
	for _, button := range catalog.Sections[0].Entries[0].Buttons {
		if button.CommandText == "/upgrade dev" {
			t.Fatalf("shipping root catalog should hide dev upgrade, got %#v", catalog.Sections[0].Entries[0].Buttons)
		}
		if strings.HasPrefix(button.CommandText, "/upgrade track ") {
			t.Fatalf("shipping root catalog should keep track switching inside the track submenu, got %#v", catalog.Sections[0].Entries[0].Buttons)
		}
	}
	if strings.Contains(summary, "/upgrade dev") || strings.Contains(summary, "本地升级产物：") || strings.Contains(summary, "/upgrade local") {
		t.Fatalf("shipping upgrade root page should hide local upgrade details, got %#v", summary)
	}
}

func TestUpgradeTrackRejectsDisallowedShippingTrack(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorShipping)

	gateway := newLifecycleGateway()
	app, statePath := newUpgradeTestApp(t, gateway)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:main:chat:1",
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
		SurfaceSessionID: "feishu:main:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade local",
	})

	waitForUpgradeNoticeBody(t, gateway, "当前构建不支持 `/upgrade local`")
}

func TestUpgradeDevRejectedInShippingFlavor(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorShipping)

	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:main:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade dev",
	})

	waitForUpgradeNoticeBody(t, gateway, "当前构建不支持 `/upgrade dev`")
}

func TestBuildUpgradeStatusCatalogExposesAlphaPolicyOptions(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorAlpha)

	catalog := buildUpgradeRootPageView(install.InstallState{
		CurrentTrack:   install.ReleaseTrackAlpha,
		CurrentVersion: "v1.0.0-alpha.1",
	}, false, "", "", "")
	assertCatalogUsesPlainTextContracts(t, &catalog)
	buttons := catalog.Sections[0].Entries[0].Buttons
	if got := len(buttons); got != 3 {
		t.Fatalf("alpha quick buttons = %d, want 3", got)
	}
	foundLatest := false
	foundDev := false
	for _, button := range buttons {
		switch button.CommandText {
		case "/upgrade latest":
			foundLatest = true
		case "/upgrade dev":
			foundDev = true
		case "/upgrade local":
			t.Fatalf("alpha root catalog should hide local upgrade, got %#v", buttons)
		}
	}
	if !foundLatest || !foundDev {
		t.Fatalf("alpha root catalog missing expected buttons, got %#v", buttons)
	}
}

func TestUpgradeLocalRejectedInAlphaFlavor(t *testing.T) {
	withBuildFlavorForDaemonTest(t, buildinfo.FlavorAlpha)

	gateway := newLifecycleGateway()
	app, _ := newUpgradeTestApp(t, gateway)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		SurfaceSessionID: "feishu:main:chat:1",
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
