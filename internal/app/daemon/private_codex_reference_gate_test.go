package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateProfileDeleteAllowsUnusedAndProtectsUnloadedCheckpoint(t *testing.T) {
	host, _ := newApplicationRuntimeTestApp(t)
	create := func(name string) state.CodexProfileSummary {
		t.Helper()
		body := `{"name":"` + name + `","baseURL":"https://example.invalid/v1","apiKey":"fixture","model":"model","reasoningEffort":"high"}`
		result := performAdminRequest(t, host, http.MethodPost, "/api/admin/codex/profiles", body)
		if result.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", result.Code, result.Body.String())
		}
		var response codexProfileResponse
		if err := json.NewDecoder(result.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response.Profile
	}
	unused := create("Unused")
	deleted := performAdminRequestWithIfMatch(t, host, http.MethodDelete, "/api/admin/codex/profiles/"+unused.ID, "", unused.ETag)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("unused delete: %d %s", deleted.Code, deleted.Body.String())
	}
	used := create("Referenced")
	o := privatechat.Owner{"gateway", "unloaded-surface", "chat-unloaded", "user-unloaded", "tenant", "app"}
	isolated := privatechat.New(nil, orchestrator.Config{})
	if err := isolated.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	isolated.Service().Surface(o.SurfaceID).CodexAdmissionRef = &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: used.ID, Revision: used.Revision}, ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: used.ID, Revision: used.ContextPreference.Revision}}
	core, err := isolated.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.applications.journal.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	blocked := performAdminRequestWithIfMatch(t, host, http.MethodDelete, "/api/admin/codex/profiles/"+used.ID, "", used.ETag)
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "unloaded-surface") {
		t.Fatalf("unloaded reference not protected: %d %s", blocked.Code, blocked.Body.String())
	}
}

func TestProfileWriterAndTeamAdmissionUseOneLockOrder(t *testing.T) {
	host, _ := newApplicationRuntimeTestApp(t)
	admission := host.workSettingsAdmission(host.teamRuntime.store)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- admission(t.Context(), "", func() error { close(entered); <-release; return nil }) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("admission failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("admission did not reach commit")
	}
	writer := make(chan struct{})
	go func() {
		host.adminConfigMu.Lock()
		host.profileReferenceGate.Lock()
		host.profileReferenceGate.Unlock()
		host.adminConfigMu.Unlock()
		close(writer)
	}()
	select {
	case <-writer:
		t.Fatal("writer crossed active admission")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission deadlocked with writer")
	}
	select {
	case <-writer:
	case <-time.After(time.Second):
		t.Fatal("writer deadlocked with admission")
	}
}
