package feishu

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/xutil"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestFeishuCallBrokerRetriesRateLimitedIMCall(t *testing.T) {
	broker := NewFeishuCallBroker("app-1", NewLarkClient("", ""))
	current := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	broker.now = func() time.Time { return current }
	broker.sleep = func(_ context.Context, wait time.Duration) error {
		current = current.Add(wait)
		return nil
	}

	attempts := 0
	start := current
	resp, err := DoSDK(context.Background(), broker, CallSpec{
		GatewayID: "app-1",
		API:       "im.v1.message.create",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			ReceiveTarget: "chat_id:oc_1",
		},
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}, func(context.Context, *lark.Client) (*larkim.CreateMessageResp, error) {
		attempts++
		if attempts == 1 {
			limited := &larkim.CreateMessageResp{
				ApiResp: &larkcore.ApiResp{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"x-ogw-ratelimit-reset": []string{"0.2"},
					},
				},
				CodeError: larkcore.CodeError{Code: 99991400, Msg: "rate limited"},
			}
			return limited, newAPIError("im.v1.message.create", limited.ApiResp, limited.CodeError)
		}
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{MessageId: stringRef("om-1")},
		}, nil
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected one retry after rate-limit, got %d attempts", attempts)
	}
	if resp == nil || resp.Data == nil || xutil.StringValue(resp.Data.MessageId) != "om-1" {
		t.Fatalf("unexpected retry result: %#v", resp)
	}
	if elapsed := current.Sub(start); elapsed < 200*time.Millisecond {
		t.Fatalf("expected broker clock to advance for ratelimit cooldown, got %s", elapsed)
	}
}

func TestFeishuCallBrokerPermissionBlockShortCircuitsUntilCleared(t *testing.T) {
	broker := NewFeishuCallBroker("app-1", NewLarkClient("", ""))
	current := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	broker.now = func() time.Time { return current }
	broker.sleep = func(_ context.Context, wait time.Duration) error {
		current = current.Add(wait)
		return nil
	}

	attempts := 0
	spec := CallSpec{
		GatewayID: "app-1",
		API:       "im.v1.message.create",
		Class:     CallClassIMSend,
		Priority:  CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{
			ReceiveTarget: "chat_id:oc_1",
		},
		Retry:      RetryRateLimitOnly,
		Permission: PermissionCooldownOnly,
	}
	_, err := DoSDK(context.Background(), broker, spec, func(context.Context, *lark.Client) (*larkim.CreateMessageResp, error) {
		attempts++
		return nil, &APIError{
			API:                  "im.v1.message.create",
			Code:                 99990001,
			Msg:                  "permission denied",
			PermissionViolations: []APIErrorPermissionViolation{{Type: "tenant", Subject: "im:message"}},
			Helps:                []APIErrorHelp{{URL: "https://open.feishu.cn/permission/apply"}},
			RequestID:            "req-perm-1",
		}
	})
	if err == nil {
		t.Fatal("expected first permission error")
	}
	if attempts != 1 {
		t.Fatalf("expected first call to hit backend once, got %d", attempts)
	}

	_, err = DoSDK(context.Background(), broker, spec, func(context.Context, *lark.Client) (*larkim.CreateMessageResp, error) {
		attempts++
		return nil, nil
	})
	var blocked *PermissionBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("expected short-circuit PermissionBlockedError, got %#v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected blocked call to avoid backend hit, got %d attempts", attempts)
	}
	if gap, ok := ExtractPermissionGap(err); !ok || gap.Scope != "im:message" {
		t.Fatalf("expected blocked error to preserve permission gap, got ok=%t gap=%#v", ok, gap)
	}

	broker.ClearGrantedPermissionBlocks([]AppScopeStatus{{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1}})
	_, err = DoSDK(context.Background(), broker, spec, func(context.Context, *lark.Client) (*larkim.CreateMessageResp, error) {
		attempts++
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{MessageId: stringRef("om-1")},
		}, nil
	})
	if err != nil {
		t.Fatalf("expected cleared permission block to allow call, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected cleared call to hit backend again, got %d attempts", attempts)
	}
}
