package relayruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestAcquireLockClearsStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	if err := os.WriteFile(path, []byte(`{"pid":999999,"token":"stale","createdAt":"2026-04-05T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	lock, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer lock.Release()
}

func TestAcquireLockClearsLockWhosePIDWasReused(t *testing.T) {
	pid := os.Getpid()
	start, ok := processStartTime(pid)
	if !ok || start.Before(time.Now().Add(-24*time.Hour)) {
		t.Skip("process start time is not usable in this environment")
	}

	path := filepath.Join(t.TempDir(), "relay.lock")
	// 当前进程 PID 存活，但锁记录是很久以前创建的：说明这个 PID 是在锁创建之后
	// 才被系统复用给当前进程的，原持有者早已消失。
	createdAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	raw := `{"pid":` + strconv.Itoa(pid) + `,"token":"reused-pid","createdAt":"` + createdAt + `"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write reused-pid lock: %v", err)
	}

	lock, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatalf("AcquireLock should clear a lock whose PID was reused: %v", err)
	}
	defer lock.Release()
}

func TestAcquireLockClearsCorruptLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	// 写锁过程中被强杀会留下截断的 JSON；这种锁无法解析，同样必须清理。
	if err := os.WriteFile(path, []byte(`{"pid":`), 0o600); err != nil {
		t.Fatalf("write corrupt lock: %v", err)
	}

	lock, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatalf("AcquireLock should clear a corrupt lock file: %v", err)
	}
	defer lock.Release()
}

func TestAcquireLockFailsWhileLiveOwnerHoldsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.lock")
	first, err := AcquireLock(context.Background(), path, false)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	defer first.Release()

	second, err := AcquireLock(context.Background(), path, false)
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got lock=%v err=%v", second, err)
	}
}

func TestManagerEnsureReadySkipsStartWhenRelayIsCompatible(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "1.0.0",
			BuildFingerprint: "fp-1",
		},
		Paths: testPaths(t),
	})
	started := false
	stopped := false
	manager.probeFunc = func(context.Context) ProbeResult {
		return ProbeResult{
			Status: ProbeCompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-1",
					},
					PID: 111,
				},
			},
		}
	}
	manager.startFunc = func(context.Context) (int, error) {
		started = true
		return 0, nil
	}
	manager.stopFunc = func(context.Context, int) error {
		stopped = true
		return nil
	}

	if err := manager.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	if started || stopped {
		t.Fatalf("expected no start/stop, got started=%v stopped=%v", started, stopped)
	}
}

func TestManagerEnsureReadyStartsDaemonWhenRelayIsUnavailable(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "1.0.0",
			BuildFingerprint: "fp-1",
		},
		Paths: testPaths(t),
	})
	sequence := []ProbeResult{
		{Status: ProbeUnreachable, Err: errors.New("dial tcp 127.0.0.1:9500: connection refused")},
		{Status: ProbeUnreachable, Err: errors.New("dial tcp 127.0.0.1:9500: connection refused")},
		{
			Status: ProbeCompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-1",
					},
					PID: 222,
				},
			},
		},
	}
	index := 0
	manager.probeFunc = func(context.Context) ProbeResult {
		result := sequence[index]
		if index < len(sequence)-1 {
			index++
		}
		return result
	}
	starts := 0
	manager.startFunc = func(context.Context) (int, error) {
		starts++
		return 222, nil
	}
	manager.stopFunc = func(context.Context, int) error {
		t.Fatal("unexpected stop")
		return nil
	}

	if err := manager.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	if starts != 1 {
		t.Fatalf("expected one daemon start, got %d", starts)
	}
}

func TestManagerEnsureReadyReplacesIncompatibleDaemonDuringBootstrap(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "2.0.0",
			BuildFingerprint: "fp-new",
		},
		Paths: testPaths(t),
	})
	sequence := []ProbeResult{
		{
			Status: ProbeIncompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
					PID: 333,
				},
			},
		},
		{
			Status: ProbeIncompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
					PID: 333,
				},
			},
		},
		{
			Status: ProbeCompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "2.0.0",
						BuildFingerprint: "fp-new",
					},
					PID: 444,
				},
			},
		},
	}
	index := 0
	manager.probeFunc = func(context.Context) ProbeResult {
		result := sequence[index]
		if index < len(sequence)-1 {
			index++
		}
		return result
	}
	var stoppedPID int
	var starts int
	manager.stopFunc = func(_ context.Context, pid int) error {
		stoppedPID = pid
		return nil
	}
	manager.startFunc = func(context.Context) (int, error) {
		starts++
		return 444, nil
	}

	if err := manager.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	if stoppedPID != 333 {
		t.Fatalf("expected incompatible daemon pid 333 to be stopped, got %d", stoppedPID)
	}
	if starts != 1 {
		t.Fatalf("expected one replacement start, got %d", starts)
	}
}

func TestManagerEnsureReadyRefusesToReplaceIncompatibleDaemonWhenConfigured(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "2.0.0",
			BuildFingerprint: "fp-new",
		},
		Paths:          testPaths(t),
		MismatchAction: ProbeMismatchRefuseReplace,
	})
	manager.probeFunc = func(context.Context) ProbeResult {
		return ProbeResult{
			Status: ProbeIncompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
					PID: 333,
				},
			},
			Err: errors.New("relay version mismatch"),
		}
	}
	manager.startFunc = func(context.Context) (int, error) {
		t.Fatal("unexpected start")
		return 0, nil
	}
	manager.restartFunc = func(context.Context) error {
		t.Fatal("unexpected restart")
		return nil
	}
	manager.stopFunc = func(context.Context, int) error {
		t.Fatal("unexpected stop")
		return nil
	}

	err := manager.EnsureReady(context.Background())
	var mismatchErr ProbeMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected ProbeMismatchError, got %v", err)
	}
	if mismatchErr.Status != ProbeIncompatible {
		t.Fatalf("mismatch status = %s, want incompatible", mismatchErr.Status)
	}
}

func TestManagerEnsureReadyRefusesToReplaceRelayMissingServerIdentityWhenConfigured(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity:       testBinaryIdentity(),
		Paths:          testPaths(t),
		MismatchAction: ProbeMismatchRefuseReplace,
	})
	manager.probeFunc = func(context.Context) ProbeResult {
		return ProbeResult{
			Status: ProbeIncompatible,
			Err:    errors.New("relay welcome missing server identity"),
		}
	}
	manager.startFunc = func(context.Context) (int, error) {
		t.Fatal("unexpected start")
		return 0, nil
	}
	manager.restartFunc = func(context.Context) error {
		t.Fatal("unexpected restart")
		return nil
	}
	manager.stopFunc = func(context.Context, int) error {
		t.Fatal("unexpected stop")
		return nil
	}

	err := manager.EnsureReady(context.Background())
	var mismatchErr ProbeMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected ProbeMismatchError, got %v", err)
	}
	if mismatchErr.Status != ProbeIncompatible {
		t.Fatalf("mismatch status = %s, want incompatible", mismatchErr.Status)
	}
}

func TestManagerEnsureReadyUsesRestartHookForIncompatibleRelay(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "2.0.0",
			BuildFingerprint: "fp-new",
		},
		Paths: testPaths(t),
	})
	sequence := []ProbeResult{
		{
			Status: ProbeIncompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
					PID: 333,
				},
			},
		},
		{
			Status: ProbeIncompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
					PID: 333,
				},
			},
		},
		{
			Status: ProbeCompatible,
			Welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "2.0.0",
						BuildFingerprint: "fp-new",
					},
					PID: 444,
				},
			},
		},
	}
	index := 0
	manager.probeFunc = func(context.Context) ProbeResult {
		result := sequence[index]
		if index < len(sequence)-1 {
			index++
		}
		return result
	}
	starts := 0
	restarts := 0
	stopped := false
	manager.startFunc = func(context.Context) (int, error) {
		starts++
		return 444, nil
	}
	manager.restartFunc = func(context.Context) error {
		restarts++
		return nil
	}
	manager.stopFunc = func(context.Context, int) error {
		stopped = true
		return nil
	}

	if err := manager.EnsureReady(context.Background()); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	if restarts != 1 {
		t.Fatalf("expected one restart hook call, got %d", restarts)
	}
	if starts != 0 {
		t.Fatalf("expected restart hook to avoid fresh start, got %d starts", starts)
	}
	if stopped {
		t.Fatal("expected restart hook path to avoid PID stop")
	}
}

func TestProbeWelcomeAcceptsLegacyCommandBeforeWelcome(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read hello: %v", err)
			return
		}

		commandPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
			Type: agentproto.EnvelopeCommand,
			Command: &agentproto.Command{
				CommandID: "cmd-legacy",
				Kind:      agentproto.CommandThreadsRefresh,
			},
		})
		if err != nil {
			t.Errorf("marshal command: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, commandPayload); err != nil {
			t.Errorf("write command: %v", err)
			return
		}

		welcomePayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
			Type: agentproto.EnvelopeWelcome,
			Welcome: &agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-legacy",
					},
					PID: 123,
				},
			},
		})
		if err != nil {
			t.Errorf("marshal welcome: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, welcomePayload); err != nil {
			t.Errorf("write welcome: %v", err)
		}
	}))
	defer httpServer.Close()

	welcome, status, err := probeWelcome(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), probeHello(agentproto.BinaryIdentity{
		Product:          ProductName,
		Version:          "1.0.0",
		BuildFingerprint: "fp-legacy",
	}))
	if err != nil {
		t.Fatalf("probeWelcome: %v", err)
	}
	if status != ProbeCompatible {
		t.Fatalf("status = %s, want %s", status, ProbeCompatible)
	}
	if welcome.Server == nil || welcome.Server.BuildFingerprint != "fp-legacy" {
		t.Fatalf("unexpected welcome: %#v", welcome)
	}
}

func TestProbeHelloMarksProbeMode(t *testing.T) {
	hello := probeHello(agentproto.BinaryIdentity{
		Product:          ProductName,
		Version:          "1.0.0",
		BuildFingerprint: "fp-1",
	})
	if !hello.Probe {
		t.Fatal("expected probe hello to set Probe")
	}
	if hello.Instance.InstanceID != "probe" {
		t.Fatalf("probe instance id = %q, want probe", hello.Instance.InstanceID)
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	base := t.TempDir()
	return Paths{
		StateDir:        base,
		LogsDir:         filepath.Join(base, "logs"),
		DaemonLogFile:   filepath.Join(base, "logs", "relayd.log"),
		ManagerLockFile: filepath.Join(base, "relay-manager.lock"),
		DaemonLockFile:  filepath.Join(base, "relayd.lock"),
		PIDFile:         filepath.Join(base, "codex-remote-relayd.pid"),
		IdentityFile:    filepath.Join(base, "codex-remote-relayd.identity.json"),
	}
}

func TestManagerWelcomeCompatible(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "1.0.0",
			BuildFingerprint: "fp-1",
		},
		Paths: testPaths(t),
	})
	if !manager.WelcomeCompatible(agentproto.Welcome{
		Protocol: agentproto.WireProtocol,
		Server: &agentproto.ServerIdentity{
			BinaryIdentity: agentproto.BinaryIdentity{
				Product:          ProductName,
				Version:          "1.0.0",
				BuildFingerprint: "fp-1",
			},
		},
	}) {
		t.Fatal("expected welcome to be compatible")
	}
	if manager.WelcomeCompatible(agentproto.Welcome{
		Protocol: agentproto.WireProtocol,
		Server: &agentproto.ServerIdentity{
			BinaryIdentity: agentproto.BinaryIdentity{
				Product:          ProductName,
				Version:          "1.0.1",
				BuildFingerprint: "fp-2",
			},
			StartedAt: time.Now(),
		},
	}) {
		t.Fatal("expected welcome to be incompatible")
	}
}
