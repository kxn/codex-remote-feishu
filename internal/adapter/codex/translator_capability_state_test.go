package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerCapabilityStateNotificationsProduceStateOnlyEvents(t *testing.T) {
	tr := NewTranslator("inst-1")

	tests := []struct {
		name   string
		frame  string
		assert func(t *testing.T, event agentproto.Event)
	}{
		{
			name:  "skills changed",
			frame: `{"method":"skills/changed","params":{}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				if event.CapabilityState == nil || event.CapabilityState.Method != "skills/changed" || !event.CapabilityState.SkillsChanged {
					t.Fatalf("unexpected skills state: %#v", event)
				}
			},
		},
		{
			name:  "mcp startup status",
			frame: `{"method":"mcpServer/startupStatus/updated","params":{"threadId":"thread-1","name":"docs","status":"failed","error":"login required","failureReason":"reauthenticationRequired"}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if event.ThreadID != "thread-1" || state == nil || state.MCPServerStartupStatus == nil {
					t.Fatalf("unexpected mcp startup state: %#v", event)
				}
				status := state.MCPServerStartupStatus
				if status.Name != "docs" || status.Status != "failed" || status.Error != "login required" || status.FailureReason != "reauthenticationRequired" {
					t.Fatalf("unexpected mcp startup payload: %#v", status)
				}
			},
		},
		{
			name:  "mcp oauth completion without pending flow",
			frame: `{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread-1","success":false,"error":"callback timed out"}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if event.ThreadID != "thread-1" || state == nil || state.MCPOAuthLoginCompleted == nil {
					t.Fatalf("unexpected mcp oauth state: %#v", event)
				}
				completed := state.MCPOAuthLoginCompleted
				if completed.Name != "docs" || completed.Success || completed.Error != "callback timed out" {
					t.Fatalf("unexpected mcp oauth completion payload: %#v", completed)
				}
			},
		},
		{
			name:  "app list updated",
			frame: `{"method":"app/list/updated","params":{"data":[{"id":"app-1","name":"Docs","description":"Knowledge base"},{"id":"app-2","title":"Ops"}]}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if state == nil || len(state.Apps) != 2 {
					t.Fatalf("unexpected app list state: %#v", event)
				}
				if state.Apps[0].ID != "app-1" || state.Apps[0].Name != "Docs" || state.Apps[0].Description != "Knowledge base" {
					t.Fatalf("unexpected first app: %#v", state.Apps[0])
				}
				if state.Apps[1].Name != "Ops" {
					t.Fatalf("unexpected second app title fallback: %#v", state.Apps[1])
				}
			},
		},
		{
			name:  "account updated",
			frame: `{"method":"account/updated","params":{"authMode":"chatgpt","planType":"pro"}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if state == nil || state.Account == nil || state.Account.AuthMode != "chatgpt" || state.Account.PlanType != "pro" {
					t.Fatalf("unexpected account state: %#v", event)
				}
			},
		},
		{
			name:  "account rate limits sparse update",
			frame: `{"method":"account/rateLimits/updated","params":{"rateLimits":{"primary":{"used":4,"limit":10,"resetsAt":"2026-07-17T08:00:00Z"},"secondary":{"remaining":2}}}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if state == nil || len(state.RateLimits) != 2 {
					t.Fatalf("unexpected rate limit state: %#v", event)
				}
				if state.RateLimits["primary"]["used"] != float64(4) || state.RateLimits["secondary"]["remaining"] != float64(2) {
					t.Fatalf("unexpected sparse rate limits: %#v", state.RateLimits)
				}
			},
		},
		{
			name:  "account login completed",
			frame: `{"method":"account/login/completed","params":{"loginId":"login-1","success":true}}`,
			assert: func(t *testing.T, event agentproto.Event) {
				state := event.CapabilityState
				if state == nil || state.AccountLoginCompleted == nil || state.AccountLoginCompleted.LoginID != "login-1" || !state.AccountLoginCompleted.Success {
					t.Fatalf("unexpected login completion state: %#v", event)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tr.ObserveServer([]byte(tt.frame))
			if err != nil {
				t.Fatalf("observe server: %v", err)
			}
			if len(result.Events) != 1 {
				t.Fatalf("expected one state event, got %#v", result.Events)
			}
			event := result.Events[0]
			if event.Kind != agentproto.EventCapabilityStateUpdated {
				t.Fatalf("unexpected event kind: %#v", event)
			}
			if event.CapabilityState == nil {
				t.Fatalf("missing capability state payload: %#v", event)
			}
			tt.assert(t, event)
		})
	}
}
