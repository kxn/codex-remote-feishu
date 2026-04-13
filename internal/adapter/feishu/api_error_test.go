package feishu

import "testing"

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
	gap, ok := ExtractPermissionGap(&driveAPIError{
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
