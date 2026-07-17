package wrapper

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBootstrapHeadlessCodexCompletesInitializeHandshake(t *testing.T) {
	for _, source := range []string{"headless", "cron"} {
		t.Run(source, func(t *testing.T) {
			app := New(Config{
				Source:  source,
				Version: "test",
			})

			bufferedLine := mustJSONLine(t, map[string]any{
				"method": "thread/started",
				"params": map[string]any{
					"thread": map[string]any{
						"id": "thread-buffered",
					},
				},
			})
			initializeResponse := mustJSONLine(t, map[string]any{
				"id": relayBootstrapInitializeID,
				"result": map[string]any{
					"userAgent": "mockcodex/0.0.1",
				},
			})

			var childStdin bytes.Buffer
			replayedStdout, err := app.bootstrapHeadlessCodex(&childStdin, strings.NewReader(bufferedLine+initializeResponse), nil, nil)
			if err != nil {
				t.Fatalf("bootstrap headless codex: %v", err)
			}

			frames := decodeJSONLines(t, childStdin.String())
			if len(frames) != 2 {
				t.Fatalf("expected 2 bootstrap frames, got %d: %s", len(frames), childStdin.String())
			}
			if got := lookupStringFromMap(frames[0], "method"); got != "initialize" {
				t.Fatalf("expected first frame to be initialize, got %q", got)
			}
			if got := lookupStringFromMap(frames[0], "id"); got != relayBootstrapInitializeID {
				t.Fatalf("expected initialize id %q, got %q", relayBootstrapInitializeID, got)
			}
			params, _ := frames[0]["params"].(map[string]any)
			capabilities, _ := params["capabilities"].(map[string]any)
			if experimental, _ := capabilities["experimentalApi"].(bool); !experimental {
				t.Fatalf("expected experimentalApi=true, got %#v", capabilities["experimentalApi"])
			}
			methods, _ := capabilities["optOutNotificationMethods"].([]any)
			wantMethods := []string{
				"item/agentMessage/delta",
				"item/plan/delta",
				"item/reasoning/textDelta",
				"item/reasoning/summaryTextDelta",
				"item/commandExecution/outputDelta",
				"item/fileChange/outputDelta",
			}
			if !sameStringAnySlice(methods, wantMethods) {
				t.Fatalf("unexpected optOutNotificationMethods: %#v", capabilities["optOutNotificationMethods"])
			}
			for _, disallowed := range []string{
				"item/fileChange/patchUpdated",
				"command/exec/outputDelta",
				"process/outputDelta",
				"item/completed",
				"turn/completed",
				"serverRequest/resolved",
			} {
				if containsAnyString(methods, disallowed) {
					t.Fatalf("opt-out allowlist must not include %q: %#v", disallowed, methods)
				}
			}
			if got := lookupStringFromMap(frames[1], "method"); got != "initialized" {
				t.Fatalf("expected second frame to be initialized, got %q", got)
			}

			remaining, err := io.ReadAll(replayedStdout)
			if err != nil {
				t.Fatalf("read replayed stdout: %v", err)
			}
			if string(remaining) != bufferedLine {
				t.Fatalf("expected buffered stdout to be replayed, got %q", string(remaining))
			}
		})
	}
}

func TestBootstrapHeadlessCodexFailsWhenInitializeRejected(t *testing.T) {
	for _, source := range []string{"headless", "cron"} {
		t.Run(source, func(t *testing.T) {
			app := New(Config{
				Source:  source,
				Version: "test",
			})

			var childStdin bytes.Buffer
			_, err := app.bootstrapHeadlessCodex(&childStdin, strings.NewReader(mustJSONLine(t, map[string]any{
				"id": relayBootstrapInitializeID,
				"error": map[string]any{
					"message": "Not initialized",
				},
			})), nil, nil)
			if err == nil {
				t.Fatal("expected bootstrap to fail when initialize is rejected")
			}
			if !strings.Contains(err.Error(), "Not initialized") {
				t.Fatalf("expected initialize rejection in error, got %v", err)
			}
		})
	}
}

func TestSyntheticInitializeFrameSkipsNonHeadless(t *testing.T) {
	app := New(Config{
		Source:  "vscode",
		Version: "test",
	})

	frame, err := app.syntheticInitializeFrame()
	if err != nil {
		t.Fatalf("syntheticInitializeFrame: %v", err)
	}
	if len(frame) != 0 {
		t.Fatalf("expected no initialize frame for non-headless source, got %#v", string(frame))
	}
}

func TestHeadlessNotificationOptOutMethods(t *testing.T) {
	got := headlessNotificationOptOutMethods()
	want := []string{
		"item/agentMessage/delta",
		"item/plan/delta",
		"item/reasoning/textDelta",
		"item/reasoning/summaryTextDelta",
		"item/commandExecution/outputDelta",
		"item/fileChange/outputDelta",
	}
	if !sameStringSlice(got, want) {
		t.Fatalf("unexpected headless opt-out methods: %#v", got)
	}
	for _, disallowed := range []string{
		"item/fileChange/patchUpdated",
		"command/exec/outputDelta",
		"process/outputDelta",
		"item/started",
		"item/completed",
		"turn/completed",
		"serverRequest/resolved",
		"thread/status/changed",
		"warning",
		"error",
	} {
		if containsString(got, disallowed) {
			t.Fatalf("headless opt-out allowlist must not include %q: %#v", disallowed, got)
		}
	}
}

func TestNeedsSyntheticBootstrap(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "headless source",
			cfg:  Config{Source: "headless"},
			want: true,
		},
		{
			name: "cron source",
			cfg:  Config{Source: "cron"},
			want: true,
		},
		{
			name: "vscode source",
			cfg:  Config{Source: "vscode"},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			app := New(tt.cfg)
			if got := app.needsSyntheticBootstrap(); got != tt.want {
				t.Fatalf("needsSyntheticBootstrap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sameStringAnySlice(got []any, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func decodeJSONLines(t *testing.T, raw string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	frames := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("unmarshal json line %q: %v", line, err)
		}
		frames = append(frames, frame)
	}
	return frames
}
