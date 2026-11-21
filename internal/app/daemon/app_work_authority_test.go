package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/goalinterlockstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPersistentAuthorityPreservesLegacyFilesThroughIdentityReplacement(t *testing.T) {
	for _, history := range []string{"valid-history", "unreadable-history", "normalizable-history"} {
		t.Run(history, func(t *testing.T) {
			dir := t.TempDir()
			resumePath, roomPath := surfaceresume.StatePath(dir), feishuroomstate.StatePath(dir)
			if history == "unreadable-history" {
				for _, path := range []string{resumePath, roomPath} {
					if err := os.WriteFile(path, []byte("original unrecognized history"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				resume, err := surfaceresume.LoadStore(resumePath)
				if err != nil {
					t.Fatal(err)
				}
				if err = resume.Put(surfaceresume.Entry{SurfaceSessionID: "feishu:main:user:owner", GatewayID: "main", ChatID: "owner", ActorUserID: "owner"}); err != nil {
					t.Fatal(err)
				}
				rooms, err := feishuroomstate.LoadStore(roomPath)
				if err != nil {
					t.Fatal(err)
				}
				if err = rooms.Put(state.FeishuRoomStateRecord{RoomID: "feishu:chat:group", ChatID: "group", PrimaryGatewayID: "main", WorkspaceKey: filepath.Join(dir, "project")}); err != nil {
					t.Fatal(err)
				}
			}
			if history == "normalizable-history" {
				legacy := map[string]string{
					resumePath: `{"version":1,"entries":{"surface-1":{"surfaceSessionID":"surface-1","productMode":"normal","resumeThreadID":"thread-1","resumeThreadCWD":"/example/project","resumeWorkspaceKey":"/example/state","resumeHeadless":true}}}`,
					roomPath:   `{"version":1,"entries":{"feishu:chat:group":{"RoomID":"feishu:chat:group","ChatID":"group","PrimaryGatewayID":"main"}}}`,
				}
				for path, data := range legacy {
					if err := os.WriteFile(path, []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
				resume, err := surfaceresume.LoadStore(resumePath)
				if err != nil || !resume.Dirty() {
					t.Fatalf("fixture must need normalization: %v", err)
				}
				rooms, err := feishuroomstate.LoadStore(roomPath)
				if err != nil || !rooms.Dirty() {
					t.Fatalf("fixture must need normalization: %v", err)
				}
			}
			original := map[string][]byte{}
			for _, path := range []string{resumePath, roomPath} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				original[path] = data
			}
			if _, err := captureTeamLegacyFiles(dir); err != nil {
				t.Fatal(err)
			}
			app := New(":0", ":0", &fakeAdminGatewayController{}, agentproto.ServerIdentity{})
			app.SetHeadlessRuntime(HeadlessRuntimeConfig{LegacyHistoryOnly: true, Paths: relayruntime.Paths{StateDir: dir}})
			for _, appID := range []string{"old-app", "new-app", ""} {
				transition, err := app.planFeishuBotIdentityTransition("main", appID)
				if err != nil {
					t.Fatal(err)
				}
				if err = app.commitFeishuBotIdentityTransition(transition); err != nil {
					t.Fatal(err)
				}
				for path, want := range original {
					got, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("identity %s rewrote legacy file %s", appID, path)
					}
				}
			}
		})
	}
}

func TestRetiredMCPDoesNotDispatchWithPreviouslyValidCredentials(t *testing.T) {
	app := &App{headlessRuntime: HeadlessRuntimeConfig{LegacyHistoryOnly: true}}
	app.toolRuntime.BearerToken = "old-session-token"
	called := false
	handler := app.requireToolAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	request.RemoteAddr = "127.0.0.1:3456"
	request.Header.Set("Authorization", "Bearer old-session-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusGone || called {
		t.Fatalf("legacy tool still dispatched: %d %v", response.Code, called)
	}
}

func TestPersistentAuthorityPreservesRetiredWorkspaceAndGoalHistory(t *testing.T) {
	dir := t.TempDir()
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(filepath.Join(dir, "project"), agentproto.BackendClaude, "old-profile")
	raw, err := json.Marshal(map[string]any{"version": 1, "entries": map[string]any{key: map[string]string{"ReasoningEffort": "high", "AccessMode": "confirm", "PlanMode": "on"}}})
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{
		claudeworkspaceprofile.StatePath(dir): raw,
		goalinterlockstore.StatePath(dir):     []byte(`{"version":1,"entries":{"old":{"key":"old","lease":{"InstanceID":"old","ThreadID":"history"}}}}`),
	}
	for path, data := range originals {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := New(":0", ":0", &fakeAdminGatewayController{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{LegacyHistoryOnly: true, Paths: relayruntime.Paths{StateDir: dir}})
	if app.claudeWorkspaceProfileState.writable() || app.goalInterlockRuntime.writable() {
		t.Fatal("retired stores remain writable")
	}
	app.syncClaudeWorkspaceProfileStateLocked()
	app.persistGoalInterlockStateLocked(time.Unix(2, 0))
	for path, want := range originals {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("history changed: %s, %v", path, err)
		}
	}
}

func TestPersistentAuthorityRejectsLegacyPrimaryBootstrap(t *testing.T) {
	gateway := &primaryBootstrapGateway{}
	app := newPrimaryBootstrapTestApp(t, gateway)
	app.headlessRuntime.LegacyHistoryOnly = true
	app.HandleGatewayAction(context.Background(), botAddedAction("app-1", "oc_room"))
	if gateway.chatInfoCalls != 0 || len(gateway.operations) != 0 || len(app.service.FeishuRoomState()) != 0 {
		t.Fatal("retired primary bootstrap queried or changed group state")
	}
}

func TestPersistentAuthorityDoesNotWriteBackLegacyBotMirror(t *testing.T) {
	app := New(":0", ":0", &fakeAdminGatewayController{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{LegacyHistoryOnly: true, Paths: relayruntime.Paths{StateDir: t.TempDir()}})
	wanted, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: "main", Backend: agentproto.BackendCodex})
	if err := app.botCapabilitySettingsState.store.Put(wanted); err != nil {
		t.Fatal(err)
	}
	stale, _ := state.NormalizeBotCapabilitySettingsRecord(state.BotCapabilitySettingsRecord{GatewayID: "main", Backend: agentproto.BackendClaude})
	app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{stale})
	app.syncBotCapabilitySettingsStateLocked()
	got, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main"))
	if !ok || got.Backend != wanted.Backend {
		t.Fatalf("legacy mirror replaced current settings: %#v", got)
	}
}
