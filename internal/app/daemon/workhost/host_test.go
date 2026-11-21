package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type testSettings struct{ location work.ExecutionLocation }

func (s testSettings) ResolveLaunch(context.Context, work.WorkScope, work.Role, work.WorkContext) (LaunchSettings, error) {
	return LaunchSettings{Backend: work.BackendClaude, Location: s.location, ProfileID: "default", Environment: []string{"PATH=/bin", "SECRET_TEST_KEY=do-not-persist"}}, nil
}

type testRegistry struct{ store *workstore.Store }

func (r testRegistry) RegisterCarrier(ctx context.Context, carrier workstore.CarrierRegistration) error {
	return r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.RegisterCarrier(carrier) })
}

func hostFixture(t *testing.T, start func(relayruntime.HeadlessLaunchOptions) (int, error)) (*Host, *workstore.Store) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workstore.Open(ctx, filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "role", Name: "Leader", Description: "Coordinate", Revision: 1, DescriptionVersion: 1, SettingsRevision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		return tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: "human", Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	host, err := New(store, service, signer, Config{Paths: relayruntime.Paths{StateDir: root}, BinaryPath: "fake-test-binary", HostID: "local", ToolEndpoint: "http://127.0.0.1:9090", Settings: testSettings{location: work.ExecutionLocation{ID: "location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}}, Registry: testRegistry{store: store}, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	return host, store
}

func TestHostPersistsOwnerBeforeLaunchAndDoesNotStoreCredentials(t *testing.T) {
	var h *Host
	var store *workstore.Store
	calls := 0
	var launched relayruntime.HeadlessLaunchOptions
	h, store = hostFixture(t, func(options relayruntime.HeadlessLaunchOptions) (int, error) {
		calls++
		launched = options
		err := store.Within(context.Background(), func(tx *workstore.Tx) error {
			workers, err := tx.ListWorkers("scope")
			if err != nil {
				return err
			}
			if len(workers) != 1 {
				t.Fatalf("workers=%d", len(workers))
			}
			worker := workers[0]
			if worker.State != work.WorkerPreparing {
				t.Fatalf("premature ready: %s", worker.State)
			}
			if _, err = tx.GetCarrier(worker.CarrierIncarnation); err != nil {
				return err
			}
			op, err := tx.GetOperation(launchID(worker))
			if err != nil {
				return err
			}
			if op.State != work.OperationSending {
				t.Fatalf("launch before intent: %s", op.State)
			}
			body, err := tx.GetContent("scope", op.RequestRef)
			if err != nil {
				return err
			}
			if strings.Contains(string(body.Data), "do-not-persist") {
				t.Fatal("launch stored a provider credential")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return 1234, nil
	})
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || launched.LaunchMode != relayruntime.HeadlessLaunchModeClaudeAppServer {
		t.Fatalf("calls=%d launch=%s", calls, launched.LaunchMode)
	}
	var snapshotPath, token string
	for _, entry := range launched.Env {
		key, value, _ := strings.Cut(entry, "=")
		if key == config.WorkLaunchSnapshotEnv {
			snapshotPath = value
		}
		if key == config.WorkSessionTokenEnv {
			token = value
		}
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || strings.Contains(string(data), token) {
		t.Fatal("missing isolated token or token persisted in snapshot")
	}
	var snapshot agentproto.RuntimeLaunchSnapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Owner.ScopeID != "scope" || snapshot.Owner.RoleID != "role" || snapshot.CarrierIncarnation == "" || snapshot.ProcessGeneration != 1 {
		t.Fatalf("bad owner snapshot: %+v", snapshot.Owner)
	}
	if _, err = h.signer.Verify(token); err != nil {
		t.Fatal(err)
	}
}

func TestUncertainHostLaunchIsNotSpawnedAgain(t *testing.T) {
	calls := 0
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) {
		calls++
		return 0, errors.New("launcher lost its response")
	})
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(context.Background(), need); err == nil {
		t.Fatal("uncertain launch claimed success")
	}
	if err := h.Prepare(context.Background(), need); err == nil {
		t.Fatal("unknown launch did not report required recovery")
	}
	if calls != 1 {
		t.Fatalf("unknown launch spawned %d processes", calls)
	}
	err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		workers, err := tx.ListWorkers("scope")
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(launchID(workers[0]))
		if err != nil {
			return err
		}
		if op.State != work.OperationUnknown || workers[0].State == work.WorkerReady {
			t.Fatalf("unknown launch released as ready: %+v", op)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
