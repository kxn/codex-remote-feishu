package feishu

import (
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"net/http"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func TestExtractPermissionGapFromAPIError(t *testing.T) {
	gap, ok := ExtractPermissionGap(&APIError{
		API:  "im.v1.message.create",
		Code: 99990001,
		Msg:  "permission denied",
		PermissionViolations: []APIErrorPermissionViolation{
			{Type: "tenant", Subject: "drive:drive"},
		},
		Helps: []APIErrorHelp{
			{URL: "https://open.feishu.cn/permission/apply"},
		},
		RequestID: "req-1",
	})
	if !ok {
		t.Fatal("expected permission gap to be extracted")
	}
	if gap.Scope != "drive:drive" || gap.ScopeType != "tenant" {
		t.Fatalf("unexpected gap scope: %#v", gap)
	}
	if gap.ApplyURL != "https://open.feishu.cn/permission/apply" {
		t.Fatalf("unexpected apply url: %#v", gap)
	}
	if gap.SourceAPI != "im.v1.message.create" || gap.RequestID != "req-1" {
		t.Fatalf("unexpected gap metadata: %#v", gap)
	}
}

func TestExtractPermissionGapFromDriveAPIError(t *testing.T) {
	gap, ok := ExtractPermissionGap(&previewpkg.DriveAPIError{
		API:       "drive.v1.file.upload_all",
		Code:      99991672,
		Msg:       "Access denied",
		RequestID: "req-drive-1",
	})
	if !ok {
		t.Fatal("expected drive permission gap to be extracted")
	}
	if gap.Scope != "drive:drive" || gap.ScopeType != "tenant" {
		t.Fatalf("unexpected drive gap: %#v", gap)
	}
	if gap.SourceAPI != "drive.v1.file.upload_all" || gap.RequestID != "req-drive-1" {
		t.Fatalf("unexpected drive gap metadata: %#v", gap)
	}
}

func TestExtractRateLimitFromAPIError(t *testing.T) {
	resp := &larkcore.ApiResp{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"x-ogw-ratelimit-reset": []string{"0.5"},
			"Retry-After":           []string{"1"},
			larkcore.HttpHeaderKeyRequestId: []string{
				"req-rate-1",
			},
		},
	}
	err := newAPIError("im.v1.message.create", resp, larkcore.CodeError{
		Code: 99991400,
		Msg:  "rate limited",
	})
	rate, ok := ExtractRateLimit(err)
	if !ok {
		t.Fatal("expected rate-limit evidence to be extracted")
	}
	if rate.API != "im.v1.message.create" || rate.RequestID != "req-rate-1" {
		t.Fatalf("unexpected rate-limit metadata: %#v", rate)
	}
	if rate.StatusCode != http.StatusTooManyRequests || rate.ErrorCode != 99991400 {
		t.Fatalf("unexpected rate-limit codes: %#v", rate)
	}
	if rate.RateLimitResetAfter < 450*time.Millisecond || rate.RateLimitResetAfter > 550*time.Millisecond {
		t.Fatalf("unexpected reset duration: %s", rate.RateLimitResetAfter)
	}
	if rate.RetryAfter < 950*time.Millisecond || rate.RetryAfter > 1050*time.Millisecond {
		t.Fatalf("unexpected retry-after duration: %s", rate.RetryAfter)
	}
}
