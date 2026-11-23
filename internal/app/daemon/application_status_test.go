package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestApplicationStatusIncludesPrivateAndTeamWithDetachedRecords(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "sdk-status", MessageID: "status-message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := app.applications.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := app.applications.startWorkers(t.Context(), make(chan error, 4)); err != nil {
		t.Fatal(err)
	}
	if len(gateway.waitForOperationCount(1, 3*time.Second)) != 1 {
		t.Fatal("private input was not processed")
	}
	status := app.runtimeStatusPayload(t.Context())
	if status.Authority != "separate_applications" || len(status.Surfaces) != 1 || status.Work == nil || !status.Work.Available || status.PrivateReadError != "" {
		t.Fatalf("status missing application: authority=%s surfaces=%d privateError=%s", status.Authority, len(status.Surfaces), status.PrivateReadError)
	}
	status.Surfaces[0].ActorUserID = "mutated-observer"
	next := app.runtimeStatusPayload(t.Context())
	if len(next.Surfaces) != 1 || next.Surfaces[0].ActorUserID != "user" {
		t.Fatal("administrative read exposed mutable private records")
	}
}

func TestLegacyImageCleanupCannotDeleteOwnerManagedPrivateStaging(t *testing.T) {
	app, stateDir := newImageStagingAdminTestApp(t)
	root := filepath.Join(stateDir, "image-staging", "gateway")
	legacy := writeImageStagingFile(t, filepath.Join(root, "old.png"), "unused legacy image")
	owned := writeImageStagingFile(t, filepath.Join(root, privatestaging.DirectoryName, "event", "input.png"), "pending owned image")
	manifest := writeImageStagingFile(t, filepath.Join(root, privatestaging.DirectoryName, "event", "manifest.json"), "owned manifest")
	old := time.Now().Add(-48 * time.Hour)
	for _, path := range []string{legacy, owned, manifest} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	response := performAdminRequest(t, app, http.MethodPost, "/api/admin/storage/image-staging/cleanup", `{"olderThanHours":24}`)
	if response.Code != http.StatusOK {
		t.Fatalf("cleanup status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("unused legacy image retained: %v", err)
	}
	for _, path := range []string{owned, manifest} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("owner-managed artifact removed", err)
		}
	}
}
