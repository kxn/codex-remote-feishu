package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateObservedSettingsDoNotInventMappedPermissions(t *testing.T) {
	event := agentproto.Event{Kind: agentproto.EventConfigObserved, ObservedPermission: &agentproto.ObservedPermissionState{NativeMode: "plan", ProjectedPlanMode: "on", ProjectionKind: agentproto.ObservedPermissionProjectionKindPartial}}
	access := teamPrivateObservedSettingValue(event, true)
	if !strings.Contains(access, "原生模式 plan") || !strings.Contains(access, "未完全映射") || strings.Contains(access, "完全访问") {
		t.Fatalf("invented access mapping: %s", access)
	}
	if plan := teamPrivateObservedSettingValue(event, false); plan != "计划模式开启" {
		t.Fatal(plan)
	}
	event = agentproto.Event{Kind: agentproto.EventThreadSettingsUpdated, ThreadSettings: &agentproto.ThreadSettingsUpdate{ApprovalPolicy: "on-request", Sandbox: "workspace-write"}}
	if access = teamPrivateObservedSettingValue(event, true); access != "审批策略 on-request；沙箱 workspace-write" {
		t.Fatal(access)
	}
	if plan := teamPrivateObservedSettingValue(event, false); plan != "" {
		t.Fatal("settings receipt without mode invented plan setting")
	}
}
