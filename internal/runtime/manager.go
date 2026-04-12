package relayruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type ProbeStatus string

const (
	ProbeCompatible   ProbeStatus = "compatible"
	ProbeIncompatible ProbeStatus = "incompatible"
	ProbeLegacy       ProbeStatus = "legacy"
	ProbeUnreachable  ProbeStatus = "unreachable"
	ProbeUnknown      ProbeStatus = "unknown"
)

type ProbeResult struct {
	Status  ProbeStatus
	Welcome agentproto.Welcome
	Err     error
}

type ManagerConfig struct {
	RelayServerURL       string
	Identity             agentproto.BinaryIdentity
	ConfigPath           string
	Paths                Paths
	DaemonBinaryPath     string
	DaemonUseSystemProxy bool
	CapturedProxyEnv     []string
	ProbeTimeout         time.Duration
	StartupTimeout       time.Duration
	PollInterval         time.Duration
	StartFunc            func(context.Context) (int, error)
	RestartFunc          func(context.Context) error
	StopFunc             func(context.Context, int) error
}

type Manager struct {
	config ManagerConfig

	probeFunc   func(context.Context) ProbeResult
	startFunc   func(context.Context) (int, error)
	restartFunc func(context.Context) error
	stopFunc    func(context.Context, int) error
}

func NewManager(cfg ManagerConfig) *Manager {
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 2 * time.Second
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 12 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	return &Manager{
		config:      cfg,
		startFunc:   cfg.StartFunc,
		restartFunc: cfg.RestartFunc,
		stopFunc:    cfg.StopFunc,
	}
}

func (m *Manager) EnsureReady(ctx context.Context) error {
	initialCtx, cancel := context.WithTimeout(ctx, m.config.ProbeTimeout)
	defer cancel()
	initial := m.probe(initialCtx)
	if initial.Status == ProbeCompatible {
		return nil
	}

	lock, err := AcquireLock(ctx, m.config.Paths.ManagerLockFile, true)
	if err != nil {
		return err
	}
	defer lock.Release()

	recheckCtx, recheckCancel := context.WithTimeout(ctx, m.config.ProbeTimeout)
	defer recheckCancel()
	current := m.probe(recheckCtx)
	switch current.Status {
	case ProbeCompatible:
		return nil
	case ProbeUnreachable:
		return m.startAndWait(ctx)
	case ProbeIncompatible, ProbeLegacy:
		if m.restartFunc != nil {
			return m.restartAndWait(ctx)
		}
		pid, err := m.currentDaemonPID(current)
		if err != nil {
			return err
		}
		if err := m.stopDaemon(ctx, pid); err != nil {
			return err
		}
		return m.startAndWait(ctx)
	case ProbeUnknown:
		if current.Err != nil {
			return current.Err
		}
		return errors.New("relay endpoint is occupied by an unknown service")
	default:
		if current.Err != nil {
			return current.Err
		}
		return errors.New("relay readiness check failed")
	}
}

func (m *Manager) WelcomeCompatible(welcome agentproto.Welcome) bool {
	return m.classifyWelcome(welcome).Status == ProbeCompatible
}

func (m *Manager) probe(ctx context.Context) ProbeResult {
	if m.probeFunc != nil {
		return m.probeFunc(ctx)
	}
	welcome, status, err := probeWelcome(ctx, m.config.RelayServerURL, probeHello(m.config.Identity))
	if err != nil {
		return ProbeResult{Status: status, Err: err}
	}
	result := m.classifyWelcome(welcome)
	result.Err = err
	return result
}

func (m *Manager) classifyWelcome(welcome agentproto.Welcome) ProbeResult {
	if welcome.Protocol != agentproto.WireProtocol {
		return ProbeResult{Status: ProbeUnknown, Welcome: welcome, Err: fmt.Errorf("unexpected relay protocol %q", welcome.Protocol)}
	}
	if welcome.Server == nil {
		return ProbeResult{Status: ProbeLegacy, Welcome: welcome}
	}
	server := welcome.Server.BinaryIdentity
	if server.Product != "" && server.Product != ProductName {
		return ProbeResult{Status: ProbeUnknown, Welcome: welcome, Err: fmt.Errorf("unexpected relay product %q", server.Product)}
	}
	if CompatibleIdentity(m.config.Identity, server) {
		return ProbeResult{Status: ProbeCompatible, Welcome: welcome}
	}
	if server.Product == "" && server.Version == "" && server.Branch == "" && server.BuildFingerprint == "" {
		return ProbeResult{Status: ProbeLegacy, Welcome: welcome}
	}
	return ProbeResult{Status: ProbeIncompatible, Welcome: welcome, Err: fmt.Errorf("relay version mismatch: local=%s remote=%s", identitySummary(m.config.Identity), identitySummary(server))}
}

func (m *Manager) startAndWait(ctx context.Context) error {
	if _, err := m.startDaemon(ctx); err != nil {
		return err
	}
	return m.waitForReady(ctx)
}

func (m *Manager) restartAndWait(ctx context.Context) error {
	if m.restartFunc == nil {
		return errors.New("restart hook is not configured")
	}
	if err := m.restartFunc(ctx); err != nil {
		return err
	}
	return m.waitForReady(ctx)
}

func (m *Manager) waitForReady(ctx context.Context) error {
	deadline := time.NewTimer(m.config.StartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		probeCtx, cancel := context.WithTimeout(ctx, m.config.ProbeTimeout)
		result := m.probe(probeCtx)
		cancel()
		switch result.Status {
		case ProbeCompatible:
			return nil
		case ProbeUnknown:
			if result.Err != nil {
				return result.Err
			}
			return errors.New("relay startup collided with an unknown service")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if result.Err != nil {
				return result.Err
			}
			return errors.New("timed out waiting for relay daemon to become ready")
		case <-ticker.C:
		}
	}
}

func (m *Manager) startDaemon(ctx context.Context) (int, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx)
	}
	env := config.FilterEnvWithoutProxy(os.Environ())
	if m.config.DaemonUseSystemProxy {
		env = append(env, m.config.CapturedProxyEnv...)
	}
	return StartDetachedDaemon(LaunchOptions{
		BinaryPath: m.config.DaemonBinaryPath,
		ConfigPath: m.config.ConfigPath,
		Env:        env,
		Paths:      m.config.Paths,
	})
}

func (m *Manager) stopDaemon(ctx context.Context, pid int) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, pid)
	}
	done := make(chan error, 1)
	go func() {
		done <- terminateProcess(pid, 3*time.Second)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return err
		}
		_ = os.Remove(m.config.Paths.PIDFile)
		_ = os.Remove(m.config.Paths.IdentityFile)
		return nil
	}
}

func (m *Manager) currentDaemonPID(result ProbeResult) (int, error) {
	if result.Welcome.Server != nil && result.Welcome.Server.PID > 0 {
		return result.Welcome.Server.PID, nil
	}
	if identity, err := ReadServerIdentity(m.config.Paths.IdentityFile); err == nil && identity.PID > 0 && processAlive(identity.PID) {
		return identity.PID, nil
	}
	pid, err := ReadPID(m.config.Paths.PIDFile)
	if err != nil {
		return 0, fmt.Errorf("unable to determine relay pid: %w", err)
	}
	if !processAlive(pid) {
		return 0, fmt.Errorf("relay pid %d is not running", pid)
	}
	return pid, nil
}

func probeHello(identity agentproto.BinaryIdentity) agentproto.Hello {
	return agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Probe:    true,
		Instance: agentproto.InstanceHello{
			InstanceID:       "probe",
			DisplayName:      "probe",
			WorkspaceRoot:    "",
			WorkspaceKey:     "",
			ShortName:        "probe",
			Version:          identity.Version,
			BuildFingerprint: identity.BuildFingerprint,
			BinaryPath:       identity.BinaryPath,
			PID:              os.Getpid(),
		},
	}
}

func probeWelcome(ctx context.Context, relayURL string, hello agentproto.Hello) (agentproto.Welcome, ProbeStatus, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	targetURL := normalizeRelayURL(relayURL)
	dialer := *websocket.DefaultDialer
	if relayURLUsesLoopback(targetURL) {
		dialer.Proxy = nil
	}
	conn, resp, err := dialer.DialContext(dialCtx, targetURL, http.Header{})
	if err != nil {
		if websocket.IsCloseError(err) {
			return agentproto.Welcome{}, ProbeUnknown, err
		}
		if errors.Is(err, websocket.ErrBadHandshake) || resp != nil {
			return agentproto.Welcome{}, ProbeUnknown, err
		}
		var netErr net.Error
		if errors.As(err, &netErr) || isConnectionRefused(err) {
			return agentproto.Welcome{}, ProbeUnreachable, err
		}
		return agentproto.Welcome{}, ProbeUnreachable, err
	}
	defer conn.Close()

	payload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type:  agentproto.EnvelopeHello,
		Hello: &hello,
	})
	if err != nil {
		return agentproto.Welcome{}, ProbeUnknown, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return agentproto.Welcome{}, ProbeUnknown, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return agentproto.Welcome{}, ProbeUnknown, err
	}
	for attempt := 0; attempt < 8; attempt++ {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return agentproto.Welcome{}, ProbeUnknown, err
		}
		envelope, err := agentproto.UnmarshalEnvelope(raw)
		if err != nil {
			return agentproto.Welcome{}, ProbeUnknown, err
		}
		switch envelope.Type {
		case agentproto.EnvelopeWelcome:
			if envelope.Welcome == nil {
				return agentproto.Welcome{}, ProbeUnknown, errors.New("relay handshake welcome missing payload")
			}
			return *envelope.Welcome, ProbeCompatible, nil
		case agentproto.EnvelopeError:
			if envelope.Error != nil && envelope.Error.Message != "" {
				return agentproto.Welcome{}, ProbeUnknown, errors.New(envelope.Error.Message)
			}
			return agentproto.Welcome{}, ProbeUnknown, errors.New("relay handshake returned error")
		case agentproto.EnvelopeCommand, agentproto.EnvelopePing, agentproto.EnvelopePong, agentproto.EnvelopeCommandAck, agentproto.EnvelopeEventBatch:
			continue
		default:
			return agentproto.Welcome{}, ProbeUnknown, fmt.Errorf("unexpected relay handshake envelope: %s", envelope.Type)
		}
	}
	return agentproto.Welcome{}, ProbeUnknown, errors.New("relay handshake did not produce welcome")
}

func normalizeRelayURL(raw string) string {
	if strings.HasSuffix(raw, "/ws/agent") {
		return raw
	}
	if strings.HasSuffix(raw, "/") {
		return raw + "ws/agent"
	}
	return raw + "/ws/agent"
}

func relayURLUsesLoopback(raw string) bool {
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.TrimSpace(parsed.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func identitySummary(identity agentproto.BinaryIdentity) string {
	switch {
	case identity.BuildFingerprint != "":
		return identity.BuildFingerprint
	case identity.Version != "":
		return identity.Version
	default:
		return "unknown"
	}
}

func isConnectionRefused(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection refused") || strings.Contains(text, "actively refused")
}
