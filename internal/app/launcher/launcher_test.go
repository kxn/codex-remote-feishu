package launcher

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
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
	exitCode := Main(Options{
		Args:    []string{"app-server", "--analytics-default-enabled"},
		Version: "vtest",
		Stdin:   strings.NewReader(""),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(_ context.Context, args []string, _ io.Reader, _, _ io.Writer, version string) (int, error) {
				gotArgs = append([]string(nil), args...)
				gotVersion = version
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
			RunDaemon: func(context.Context, string) error { t.Fatal("unexpected daemon run"); return nil },
			RunInstall: func(args []string, _ io.Reader, _, _ io.Writer) error {
				gotArgs = append([]string(nil), args...)
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) (int, error) {
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

func TestMainReportsDaemonError(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := Main(Options{
		Args:   []string{"daemon"},
		Stdout: &bytes.Buffer{},
		Stderr: &stderr,
		Runners: RunnerSet{
			RunDaemon: func(context.Context, string) error { return errors.New("boom") },
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) (int, error) {
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
			RunDaemon: func(context.Context, string) error {
				ran = true
				return nil
			},
			RunInstall: func([]string, io.Reader, io.Writer, io.Writer) error {
				t.Fatal("unexpected install run")
				return nil
			},
			RunWrapper: func(context.Context, []string, io.Reader, io.Writer, io.Writer, string) (int, error) {
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
