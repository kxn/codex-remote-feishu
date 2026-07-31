package codexprofile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOAuthProbeFramesAreAuthOnlyAndDisableRefresh(t *testing.T) {
	frames := OAuthProbeFrames("test-version")
	if len(frames) != 5 {
		t.Fatalf("OAuthProbeFrames() returned %d frames, want 5", len(frames))
	}

	wantMethods := []string{"initialize", "initialized", "config/read", "account/read", "getAuthStatus"}
	for index, frame := range frames {
		if frame.Method != wantMethods[index] {
			t.Fatalf("frame %d method = %q, want %q", index, frame.Method, wantMethods[index])
		}
		if frame.Method == "thread/start" || frame.Method == "thread/resume" || frame.Method == "turn/start" {
			t.Fatalf("auth-only probe must not send %q", frame.Method)
		}
	}

	assertJSONBool(t, frames[3].Params, "refreshToken", false)
	assertJSONBool(t, frames[4].Params, "includeToken", false)
	assertJSONBool(t, frames[4].Params, "refreshToken", false)
}

func TestClassifyOAuthProbeRequiresChatGPTAccountAndManagedAuth(t *testing.T) {
	tests := []struct {
		name          string
		evidence      OAuthProbeEvidence
		wantStatus    OAuthProbeStatus
		wantError     string
		wantAvailable string
	}{
		{
			name: "managed oauth",
			evidence: OAuthProbeEvidence{
				AccountType:    "chatgpt",
				AuthMethod:     "chatgpt",
				ChatGPTBaseURL: OfficialChatGPTBaseURL,
			},
			wantStatus: OAuthProbeStatusDetected,
		},
		{
			name:       "logged out",
			evidence:   OAuthProbeEvidence{},
			wantStatus: OAuthProbeStatusMissing,
		},
		{
			name: "external chatgpt tokens are not managed oauth",
			evidence: OAuthProbeEvidence{
				AccountType: "chatgpt",
				AuthMethod:  "chatgptAuthTokens",
			},
			wantStatus: OAuthProbeStatusMissing,
		},
		{
			name: "api key is not oauth",
			evidence: OAuthProbeEvidence{
				AccountType: "apiKey",
				AuthMethod:  "apikey",
			},
			wantStatus: OAuthProbeStatusMissing,
		},
		{
			name: "chatgpt account without auth mode is unknown",
			evidence: OAuthProbeEvidence{
				AccountType: "chatgpt",
			},
			wantStatus: OAuthProbeStatusUnknown,
			wantError:  ErrorOAuthProbeUnknown,
		},
		{
			name: "contradictory evidence is unknown",
			evidence: OAuthProbeEvidence{
				AccountType: "apiKey",
				AuthMethod:  "chatgpt",
			},
			wantStatus: OAuthProbeStatusUnknown,
			wantError:  ErrorOAuthProbeUnknown,
		},
		{
			name: "custom deployment remains detected but unavailable",
			evidence: OAuthProbeEvidence{
				AccountType:    "chatgpt",
				AuthMethod:     "chatgpt",
				ChatGPTBaseURL: "https://example.invalid/backend-api",
			},
			wantStatus:    OAuthProbeStatusDetected,
			wantAvailable: ErrorOAuthDeploymentUnsupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyOAuthProbe(test.evidence)
			if got.Status != test.wantStatus || got.LastProbeErrorCode != test.wantError || got.AvailabilityCode != test.wantAvailable {
				t.Fatalf("ClassifyOAuthProbe() = %#v, want status=%q error=%q availability=%q", got, test.wantStatus, test.wantError, test.wantAvailable)
			}
		})
	}
}

func TestClassifyOAuthProbeRedactsAccountAndNormalizesOfficialDeployment(t *testing.T) {
	got := ClassifyOAuthProbe(OAuthProbeEvidence{
		AccountType:    "chatgpt",
		AuthMethod:     "chatgpt",
		Email:          "  user@example.com ",
		PlanType:       "plus",
		ChatGPTBaseURL: "https://CHATGPT.com/backend-api",
	})
	if got.Status != OAuthProbeStatusDetected || got.AvailabilityCode != "" {
		t.Fatalf("ClassifyOAuthProbe() = %#v", got)
	}
	if got.AccountHint != "u***@example.com" {
		t.Fatalf("AccountHint = %q, want redacted email", got.AccountHint)
	}
	if got.PlanType != "plus" {
		t.Fatalf("PlanType = %q, want plus", got.PlanType)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if string(raw) == "" || strings.Contains(string(raw), "user@example.com") {
		t.Fatalf("serialized probe result leaked full account: %s", raw)
	}
}

func assertJSONBool(t *testing.T, raw json.RawMessage, key string, want bool) {
	t.Helper()
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	got, ok := params[key].(bool)
	if !ok || got != want {
		t.Fatalf("params[%q] = %#v, want %t", key, params[key], want)
	}
}
