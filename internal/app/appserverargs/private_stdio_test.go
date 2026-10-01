package appserverargs

import (
	"reflect"
	"testing"
)

func TestPrivateStdioArgsRejectsSharedServerLaunches(t *testing.T) {
	for _, args := range [][]string{
		{"app-server", "daemon", "start"},
		{"app-server", "daemon", "update", "--from-cli"},
		{"app-server", "proxy"},
		{"app-server", "--listen", "unix://"},
		{"app-server", "--listen=unix:///tmp/codex.sock"},
		{"app-server", "--listen=ws://127.0.0.1:1234"},
		{"app-server", "--listen=off"},
		{"app-server", "--remote-control"},
		{"app-server", "--remote-control=true"},
		{"app-server", "--managed-daemon"},
		{"app-server", "--config", "x=1", "daemon", "start"},
		{"app-server", "--", "daemon", "start"},
		{"app-server", "--listen"},
	} {
		if _, err := PrivateStdioArgs(args); err == nil {
			t.Errorf("allowed shared/invalid launch: %v", args)
		}
	}
}

func TestPrivateStdioArgsPreservesOptionsAndValues(t *testing.T) {
	for _, args := range [][]string{
		{"app-server"},
		{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled"},
		{"app-server", "-c", "daemon", "--config=--remote-control"},
		{"app-server", "--code-mode-host", "https://localhost:1234"},
	} {
		original := append([]string{}, args...)
		got, err := PrivateStdioArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		want := append(append([]string{}, args...), "--listen=stdio://")
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(args, original) {
			t.Fatalf("got %v, want %v; input %v", got, want, args)
		}
	}
	for _, args := range [][]string{
		{"app-server", "--stdio"}, {"app-server", "--listen", "stdio://"},
		{"app-server", "--listen=stdio://"}, {"claude-app-server", "--verbose"},
	} {
		got, err := PrivateStdioArgs(args)
		if err != nil || !reflect.DeepEqual(got, args) {
			t.Fatalf("got %v, %v; want %v", got, err, args)
		}
	}
}

func TestPrivateStdioEnvOverridesEnrollmentWithoutLosingCredentials(t *testing.T) {
	base := []string{"CODEX_HOME=/original", "OPENAI_API_KEY=credential", "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=0", "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=0"}
	got := PrivateStdioEnv(base)
	want := []string{"CODEX_HOME=/original", "OPENAI_API_KEY=credential", "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=1"}
	if !reflect.DeepEqual(got, want) || base[2] != "CODEX_INTERNAL_APP_SERVER_REMOTE_CONTROL_DISABLED=0" {
		t.Fatalf("got %v, input mutated: %v", got, base)
	}
}
