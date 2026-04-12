package launcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Decision
		wantErr string
	}{
		{
			name: "app server enters wrapper",
			args: []string{"app-server", "--analytics-default-enabled"},
			want: Decision{Role: RoleWrapper, Args: []string{"app-server", "--analytics-default-enabled"}},
		},
		{
			name: "explicit wrapper app server enters wrapper",
			args: []string{"wrapper", "app-server", "--analytics-default-enabled"},
			want: Decision{Role: RoleWrapper, Args: []string{"app-server", "--analytics-default-enabled"}},
		},
		{
			name: "daemon role",
			args: []string{"daemon"},
			want: Decision{Role: RoleDaemon},
		},
		{
			name: "install role",
			args: []string{"install", "-interactive"},
			want: Decision{Role: RoleInstall, Args: []string{"-interactive"}},
		},
		{
			name: "local upgrade role",
			args: []string{"local-upgrade", "-slot", "local-test"},
			want: Decision{Role: RoleLocalUpgrade, Args: []string{"-slot", "local-test"}},
		},
		{
			name: "service role",
			args: []string{"service", "status"},
			want: Decision{Role: RoleService, Args: []string{"status"}},
		},
		{
			name: "version role",
			args: []string{"version"},
			want: Decision{Role: RoleVersion},
		},
		{
			name: "empty args defaults to daemon",
			args: nil,
			want: Decision{Role: RoleDaemon},
		},
		{
			name:    "resume rejected",
			args:    []string{"resume", "--thread", "abc"},
			wantErr: "unsupported command",
		},
		{
			name:    "wrapper resume rejected",
			args:    []string{"wrapper", "resume", "--thread", "abc"},
			wantErr: "wrapper only supports app-server mode",
		},
		{
			name:    "daemon extra arg rejected",
			args:    []string{"daemon", "extra"},
			wantErr: "daemon does not accept extra arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Detect(%v) error = %v, want substring %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Detect(%v): %v", tt.args, err)
			}
			if got.Role != tt.want.Role {
				t.Fatalf("Detect(%v) role = %q, want %q", tt.args, got.Role, tt.want.Role)
			}
			if strings.Join(got.Args, "\x00") != strings.Join(tt.want.Args, "\x00") {
				t.Fatalf("Detect(%v) args = %#v, want %#v", tt.args, got.Args, tt.want.Args)
			}
		})
	}
}

func TestMainRoutesToWrapper(t *testing.T) {
	var gotArgs []string
	var gotVersion string
	var gotBranch string
	exitCode := Main(Options{
		Args:    []string{"app-server", "--analytics-default-enabled"},
		Version: "vtest",
		Branch:  "release/1.5",
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer, string) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(_ context.Context, args []string, _ io.Reader, _, _ io.Writer, version, branch string) (int, error) {
				gotArgs = append([]string(nil), args...)
				gotVersion = version
				gotBranch = branch
				return 7, nil
			},
		},
	})
	if exitCode != 7 {
		t.Fatalf("Main exitCode = %d, want 7", exitCode)
	}
	if strings.Join(gotArgs, "\x00") != strings.Join([]string{"app-server", "--analytics-default-enabled"}, "\x00") {
		t.Fatalf("wrapper args = %#v", gotArgs)
	}
	if gotVersion != "vtest" {
		t.Fatalf("wrapper version = %q, want vtest", gotVersion)
	}
	if gotBranch != "release/1.5" {
		t.Fatalf("wrapper branch = %q, want release/1.5", gotBranch)
	}
}

func TestMainWritesUsageForInvalidArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Main(Options{
		Args:   []string{"resume", "--thread", "abc"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if exitCode != 2 {
		t.Fatalf("Main exitCode = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "unsupported command") {
		t.Fatalf("stderr = %q, want unsupported command", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage text", stderr.String())
	}
}

func TestMainRunsInstall(t *testing.T) {
	var gotArgs []string
	exitCode := Main(Options{
		Args:   []string{"install", "-interactive"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func(args []string, _ io.Reader, _, _ io.Writer, version string) error {
				gotArgs = append([]string(nil), args...)
				if version != "dev" {
					t.Fatalf("install version = %q, want dev", version)
				}
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error) {
				t.Fatal("unexpected wrapper run")
				return 0, nil
			},
		},
	})
	if exitCode != 0 {
		t.Fatalf("Main exitCode = %d, want 0", exitCode)
	}
	if strings.Join(gotArgs, "\x00") != strings.Join([]string{"-interactive"}, "\x00") {
		t.Fatalf("install args = %#v", gotArgs)
	}
}

func TestMainRunsLocalUpgrade(t *testing.T) {
	var gotArgs []string
	exitCode := Main(Options{
		Args:   []string{"local-upgrade", "-slot", "local-test"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer, string) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunLocalUpgrade: func(args []string, _ io.Reader, _, _ io.Writer, version string) error {
				gotArgs = append([]string(nil), args...)
				if version != "dev" {
					t.Fatalf("local upgrade version = %q, want dev", version)
				}
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error) {
				t.Fatal("unexpected wrapper run")
				return 0, nil
			},
		},
	})
	if exitCode != 0 {
		t.Fatalf("Main exitCode = %d, want 0", exitCode)
	}
	if strings.Join(gotArgs, "\x00") != strings.Join([]string{"-slot", "local-test"}, "\x00") {
		t.Fatalf("local upgrade args = %#v", gotArgs)
	}
}

func TestMainRunsService(t *testing.T) {
	var gotArgs []string
	exitCode := Main(Options{
		Args:   []string{"service", "status"},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer, string) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunService: func(args []string, _ io.Reader, _, _ io.Writer, version string) error {
				gotArgs = append([]string(nil), args...)
				if version != "dev" {
					t.Fatalf("service version = %q, want dev", version)
				}
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error) {
				t.Fatal("unexpected wrapper run")
				return 0, nil
			},
		},
	})
	if exitCode != 0 {
		t.Fatalf("Main exitCode = %d, want 0", exitCode)
	}
	if strings.Join(gotArgs, "\x00") != strings.Join([]string{"status"}, "\x00") {
		t.Fatalf("service args = %#v", gotArgs)
	}
}

func TestMainReportsDaemonError(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := Main(Options{
		Args:   []string{"daemon"},
		Stdout: &bytes.Buffer{},
		Stderr: &stderr,
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error { return errors.New("boom") },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer, string) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error) {
				t.Fatal("unexpected wrapper run")
				return 0, nil
			},
		},
	})
	if exitCode != 1 {
		t.Fatalf("Main exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "daemon error: boom") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMainRunsDaemonForEmptyArgs(t *testing.T) {
	ran := false
	exitCode := Main(Options{
		Args:   nil,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string, string) error {
				ran = true
				return nil
			},
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer, string) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error) {
				t.Fatal("unexpected wrapper run")
				return 0, nil
			},
		},
	})
	if exitCode != 0 {
		t.Fatalf("Main exitCode = %d, want 0", exitCode)
	}
	if !ran {
		t.Fatal("expected daemon runner to be called")
	}
}

func TestNewMainContextCancelsWhenPlatformBridgeFires(t *testing.T) {
	original := registerPlatformShutdownBridge
	defer func() {
		registerPlatformShutdownBridge = original
	}()

	var bridgeCancel context.CancelFunc
	registerPlatformShutdownBridge = func(cancel context.CancelFunc) (func(), error) {
		bridgeCancel = cancel
		return nil, nil
	}

	ctx, stop, err := newMainContext(context.Background())
	if err != nil {
		t.Fatalf("newMainContext() error = %v", err)
	}
	defer stop()
	if bridgeCancel == nil {
		t.Fatal("expected platform bridge to receive cancel function")
	}

	bridgeCancel()

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}
	if err := ctx.Err(); err == nil {
		t.Fatal("expected context to be canceled by platform bridge")
	}
}

func TestNewMainContextRunsBridgeCleanupOnStop(t *testing.T) {
	original := registerPlatformShutdownBridge
	defer func() {
		registerPlatformShutdownBridge = original
	}()

	cleanupCalled := false
	registerPlatformShutdownBridge = func(context.CancelFunc) (func(), error) {
		return func() {
			cleanupCalled = true
		}, nil
	}

	_, stop, err := newMainContext(context.Background())
	if err != nil {
		t.Fatalf("newMainContext() error = %v", err)
	}
	stop()
	if !cleanupCalled {
		t.Fatal("expected stop to unregister platform bridge")
	}
}

func TestMainReportsSignalSetupError(t *testing.T) {
	original := registerPlatformShutdownBridge
	defer func() {
		registerPlatformShutdownBridge = original
	}()

	registerPlatformShutdownBridge = func(context.CancelFunc) (func(), error) {
		return nil, errors.New("bridge setup failed")
	}

	var stderr bytes.Buffer
	exitCode := Main(Options{
		Args:   []string{"daemon"},
		Stdout: &bytes.Buffer{},
		Stderr: &stderr,
	})
	if exitCode != 1 {
		t.Fatalf("Main exitCode = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "signal setup error: bridge setup failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
