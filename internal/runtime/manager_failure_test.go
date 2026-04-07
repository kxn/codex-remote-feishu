package relayruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestManagerEnsureReadyReturnsUnknownServiceError(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: testBinaryIdentity(),
		Paths:    testPaths(t),
	})
	manager.probeFunc = func(context.Context) ProbeResult {
		return ProbeResult{Status: ProbeUnknown}
	}
	manager.startFunc = func(context.Context) (int, error) {
		t.Fatal("unexpected start")
		return 0, nil
	}
	manager.stopFunc = func(context.Context, int) error {
		t.Fatal("unexpected stop")
		return nil
	}

	err := manager.EnsureReady(context.Background())
	if err == nil || err.Error() != "relay endpoint is occupied by an unknown service" {
		t.Fatalf("EnsureReady error = %v, want unknown service error", err)
	}
}

func TestManagerEnsureReadyPropagatesUnknownProbeError(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: testBinaryIdentity(),
		Paths:    testPaths(t),
	})
	want := errors.New("unexpected relay product")
	manager.probeFunc = func(context.Context) ProbeResult {
		return ProbeResult{Status: ProbeUnknown, Err: want}
	}

	err := manager.EnsureReady(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("EnsureReady error = %v, want %v", err, want)
	}
}

func TestManagerStartAndWaitFailurePaths(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		manager := NewManager(ManagerConfig{
			Identity:       testBinaryIdentity(),
			Paths:          testPaths(t),
			StartupTimeout: 80 * time.Millisecond,
			PollInterval:   5 * time.Millisecond,
			ProbeTimeout:   5 * time.Millisecond,
		})
		manager.probeFunc = func(context.Context) ProbeResult {
			return ProbeResult{Status: ProbeUnreachable}
		}
		manager.startFunc = func(context.Context) (int, error) {
			return 123, nil
		}

		err := manager.startAndWait(context.Background())
		if err == nil || err.Error() != "timed out waiting for relay daemon to become ready" {
			t.Fatalf("startAndWait error = %v, want timeout", err)
		}
	})

	t.Run("collision", func(t *testing.T) {
		manager := NewManager(ManagerConfig{
			Identity:       testBinaryIdentity(),
			Paths:          testPaths(t),
			StartupTimeout: 80 * time.Millisecond,
			PollInterval:   5 * time.Millisecond,
			ProbeTimeout:   5 * time.Millisecond,
		})
		manager.probeFunc = func(context.Context) ProbeResult {
			return ProbeResult{Status: ProbeUnknown}
		}
		manager.startFunc = func(context.Context) (int, error) {
			return 123, nil
		}

		err := manager.startAndWait(context.Background())
		if err == nil || err.Error() != "relay startup collided with an unknown service" {
			t.Fatalf("startAndWait error = %v, want collision error", err)
		}
	})
}

func TestManagerCurrentDaemonPIDFallbacks(t *testing.T) {
	t.Run("welcome pid", func(t *testing.T) {
		manager := NewManager(ManagerConfig{
			Identity: testBinaryIdentity(),
			Paths:    testPaths(t),
		})
		pid, err := manager.currentDaemonPID(ProbeResult{
			Welcome: agentproto.Welcome{
				Server: &agentproto.ServerIdentity{PID: 321},
			},
		})
		if err != nil || pid != 321 {
			t.Fatalf("currentDaemonPID = (%d, %v), want (321, nil)", pid, err)
		}
	})

	t.Run("identity file", func(t *testing.T) {
		paths := testPaths(t)
		manager := NewManager(ManagerConfig{
			Identity: testBinaryIdentity(),
			Paths:    paths,
		})
		err := WriteServerIdentity(paths.IdentityFile, agentproto.ServerIdentity{
			BinaryIdentity: testBinaryIdentity(),
			PID:            os.Getpid(),
		})
		if err != nil {
			t.Fatalf("WriteServerIdentity: %v", err)
		}

		pid, err := manager.currentDaemonPID(ProbeResult{})
		if err != nil || pid != os.Getpid() {
			t.Fatalf("currentDaemonPID = (%d, %v), want current pid", pid, err)
		}
	})

	t.Run("pid file", func(t *testing.T) {
		paths := testPaths(t)
		manager := NewManager(ManagerConfig{
			Identity: testBinaryIdentity(),
			Paths:    paths,
		})
		if err := os.WriteFile(paths.IdentityFile, []byte("{broken"), 0o644); err != nil {
			t.Fatalf("write broken identity file: %v", err)
		}
		if err := WritePID(paths.PIDFile, os.Getpid()); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		pid, err := manager.currentDaemonPID(ProbeResult{})
		if err != nil || pid != os.Getpid() {
			t.Fatalf("currentDaemonPID = (%d, %v), want current pid", pid, err)
		}
	})

	t.Run("dead pid", func(t *testing.T) {
		paths := testPaths(t)
		manager := NewManager(ManagerConfig{
			Identity: testBinaryIdentity(),
			Paths:    paths,
		})
		if err := WritePID(paths.PIDFile, -1); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		_, err := manager.currentDaemonPID(ProbeResult{})
		if err == nil || !strings.Contains(err.Error(), "relay pid -1 is not running") {
			t.Fatalf("currentDaemonPID error = %v, want dead pid error", err)
		}
	})

	t.Run("missing pid file", func(t *testing.T) {
		manager := NewManager(ManagerConfig{
			Identity: testBinaryIdentity(),
			Paths:    testPaths(t),
		})

		_, err := manager.currentDaemonPID(ProbeResult{})
		if err == nil || !strings.Contains(err.Error(), "unable to determine relay pid") {
			t.Fatalf("currentDaemonPID error = %v, want missing pid error", err)
		}
	})
}

func TestManagerStopDaemonRemovesRuntimeFiles(t *testing.T) {
	paths := testPaths(t)
	manager := NewManager(ManagerConfig{
		Identity: testBinaryIdentity(),
		Paths:    paths,
	})
	if err := os.WriteFile(paths.PIDFile, []byte("123\n"), 0o644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	if err := os.WriteFile(paths.IdentityFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write identity file: %v", err)
	}

	if err := manager.stopDaemon(context.Background(), 0); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
	if _, err := os.Stat(paths.PIDFile); !os.IsNotExist(err) {
		t.Fatalf("expected pid file removed, stat err=%v", err)
	}
	if _, err := os.Stat(paths.IdentityFile); !os.IsNotExist(err) {
		t.Fatalf("expected identity file removed, stat err=%v", err)
	}
}

func TestManagerClassifyWelcomeStatuses(t *testing.T) {
	manager := NewManager(ManagerConfig{
		Identity: agentproto.BinaryIdentity{
			Product:          ProductName,
			Version:          "2.0.0",
			BuildFingerprint: "fp-new",
		},
		Paths: testPaths(t),
	})

	tests := []struct {
		name       string
		welcome    agentproto.Welcome
		wantStatus ProbeStatus
		wantErr    string
	}{
		{
			name:       "unexpected protocol",
			welcome:    agentproto.Welcome{Protocol: "other"},
			wantStatus: ProbeUnknown,
			wantErr:    "unexpected relay protocol",
		},
		{
			name:       "legacy missing server",
			welcome:    agentproto.Welcome{Protocol: agentproto.WireProtocol},
			wantStatus: ProbeLegacy,
		},
		{
			name: "unexpected product",
			welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{Product: "other-product"},
				},
			},
			wantStatus: ProbeUnknown,
			wantErr:    "unexpected relay product",
		},
		{
			name: "legacy empty identity",
			welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server:   &agentproto.ServerIdentity{},
			},
			wantStatus: ProbeLegacy,
		},
		{
			name: "incompatible",
			welcome: agentproto.Welcome{
				Protocol: agentproto.WireProtocol,
				Server: &agentproto.ServerIdentity{
					BinaryIdentity: agentproto.BinaryIdentity{
						Product:          ProductName,
						Version:          "1.0.0",
						BuildFingerprint: "fp-old",
					},
				},
			},
			wantStatus: ProbeIncompatible,
			wantErr:    "relay version mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.classifyWelcome(tt.welcome)
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", result.Status, tt.wantStatus)
			}
			if tt.wantErr == "" {
				if result.Err != nil {
					t.Fatalf("unexpected error: %v", result.Err)
				}
				return
			}
			if result.Err == nil || !strings.Contains(result.Err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", result.Err, tt.wantErr)
			}
		})
	}
}

func TestProbeWelcomeFailurePaths(t *testing.T) {
	tests := []struct {
		name       string
		frames     [][]byte
		wantStatus ProbeStatus
		wantErr    string
	}{
		{
			name: "error envelope",
			frames: [][]byte{mustMarshalEnvelope(t, agentproto.Envelope{
				Type: agentproto.EnvelopeError,
				Error: &agentproto.ErrorEnvelope{
					Message: "handshake rejected",
				},
			})},
			wantStatus: ProbeUnknown,
			wantErr:    "handshake rejected",
		},
		{
			name:       "welcome missing payload",
			frames:     [][]byte{[]byte(`{"type":"welcome"}`)},
			wantStatus: ProbeUnknown,
			wantErr:    "welcome missing payload",
		},
		{
			name:       "unexpected envelope",
			frames:     [][]byte{[]byte(`{"type":"hello"}`)},
			wantStatus: ProbeUnknown,
			wantErr:    "unexpected relay handshake envelope",
		},
		{
			name:       "no welcome after retries",
			frames:     repeatedFrame([]byte(`{"type":"ping"}`), 8),
			wantStatus: ProbeUnknown,
			wantErr:    "did not produce welcome",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relayURL := startProbeServer(t, tt.frames)
			_, status, err := probeWelcome(context.Background(), relayURL, probeHello(testBinaryIdentity()))
			if status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", status, tt.wantStatus)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestProbeWelcomeClassifiesHandshakeErrors(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain http"))
	}))
	defer httpServer.Close()

	_, status, err := probeWelcome(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), probeHello(testBinaryIdentity()))
	if status != ProbeUnknown {
		t.Fatalf("status = %s, want %s", status, ProbeUnknown)
	}
	if err == nil {
		t.Fatal("expected handshake error")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	_, status, err = probeWelcome(context.Background(), "ws://"+addr, probeHello(testBinaryIdentity()))
	if status != ProbeUnreachable {
		t.Fatalf("status = %s, want %s", status, ProbeUnreachable)
	}
	if err == nil {
		t.Fatal("expected unreachable error")
	}
}

func TestRuntimeHelperUtilities(t *testing.T) {
	if got := normalizeRelayURL("ws://127.0.0.1:9100"); got != "ws://127.0.0.1:9100/ws/agent" {
		t.Fatalf("normalizeRelayURL = %q", got)
	}
	if got := normalizeRelayURL("ws://127.0.0.1:9100/"); got != "ws://127.0.0.1:9100/ws/agent" {
		t.Fatalf("normalizeRelayURL with slash = %q", got)
	}
	if got := normalizeRelayURL("ws://127.0.0.1:9100/ws/agent"); got != "ws://127.0.0.1:9100/ws/agent" {
		t.Fatalf("normalizeRelayURL preserved = %q", got)
	}

	if !relayURLUsesLoopback("ws://localhost:9100/ws/agent") {
		t.Fatal("expected localhost to be loopback")
	}
	if !relayURLUsesLoopback("ws://127.0.0.1:9100/ws/agent") {
		t.Fatal("expected 127.0.0.1 to be loopback")
	}
	if relayURLUsesLoopback("not a url") {
		t.Fatal("expected invalid url to be non-loopback")
	}
	if relayURLUsesLoopback("ws://example.com/ws/agent") {
		t.Fatal("expected example.com to be non-loopback")
	}

	if got := identitySummary(agentproto.BinaryIdentity{BuildFingerprint: "fp-1"}); got != "fp-1" {
		t.Fatalf("identitySummary fingerprint = %q", got)
	}
	if got := identitySummary(agentproto.BinaryIdentity{Version: "1.2.3"}); got != "1.2.3" {
		t.Fatalf("identitySummary version = %q", got)
	}
	if got := identitySummary(agentproto.BinaryIdentity{}); got != "unknown" {
		t.Fatalf("identitySummary unknown = %q", got)
	}

	if !isConnectionRefused(errors.New("dial tcp: connection refused")) {
		t.Fatal("expected connection refused to match")
	}
	if !isConnectionRefused(errors.New("actively refused by target host")) {
		t.Fatal("expected actively refused to match")
	}
	if isConnectionRefused(errors.New("i/o timeout")) {
		t.Fatal("expected timeout not to match connection refused")
	}
}

func testBinaryIdentity() agentproto.BinaryIdentity {
	return agentproto.BinaryIdentity{
		Product:          ProductName,
		Version:          "1.0.0",
		BuildFingerprint: "fp-1",
	}
}

func mustMarshalEnvelope(t *testing.T, envelope agentproto.Envelope) []byte {
	t.Helper()
	raw, err := agentproto.MarshalEnvelope(envelope)
	if err != nil {
		t.Fatalf("MarshalEnvelope: %v", err)
	}
	return raw
}

func repeatedFrame(frame []byte, count int) [][]byte {
	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, frame)
	}
	return out
}

func startProbeServer(t *testing.T, frames [][]byte) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		for _, frame := range frames {
			if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				t.Errorf("write frame: %v", err)
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}
