package daemon

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func appendTeamPrivateObservedSettings(tx *workstore.Tx, worker work.WorkerSession, result *teamPrivateStatus) error {
	observed, err := tx.ReadPrivateNativeSettings(worker)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		label       string
		observation *workstore.PrivateNativeSettingObservation
		permission  bool
	}{{"权限", observed.Permission, true}, {"模式", observed.Plan, false}} {
		value := ""
		if field.observation != nil {
			value = teamPrivateObservedSettingValue(field.observation.Event, field.permission)
		}
		line := "最近观察到的" + field.label + "：尚无原生回执"
		if value != "" {
			line = "最近观察到的" + field.label + "：" + value + "（原生会话设置回执，" + field.observation.ObservedAt.Format(time.RFC3339) + "）"
		}
		result.Lines = append(result.Lines, line)
	}
	return nil
}

func teamPrivateObservedSettingValue(event agentproto.Event, permission bool) string {
	observed := agentproto.CloneObservedPermissionState(event.ObservedPermission)
	if !permission {
		mode := event.PlanMode
		if observed != nil && observed.ProjectedPlanMode != "" {
			mode = observed.ProjectedPlanMode
		}
		switch mode {
		case "on":
			return "计划模式开启"
		case "off":
			return "计划模式关闭"
		}
		return ""
	}
	if observed != nil {
		value := agentproto.DisplayAccessModeShort(observed.ProjectedAccessMode)
		if observed.ProjectedAccessMode == "" {
			value = ""
		}
		if observed.NativeMode != "" {
			if value != "" {
				value += "；"
			}
			value += "原生模式 " + observed.NativeMode
		}
		if observed.ProjectionKind == agentproto.ObservedPermissionProjectionKindPartial || observed.ProjectionKind == agentproto.ObservedPermissionProjectionKindUnmapped {
			value += "（未完全映射为本地权限）"
		}
		return value
	}
	if event.ThreadSettings != nil {
		var values []string
		if event.ThreadSettings.ApprovalPolicy != "" {
			values = append(values, "审批策略 "+event.ThreadSettings.ApprovalPolicy)
		}
		if event.ThreadSettings.Sandbox != "" {
			values = append(values, "沙箱 "+event.ThreadSettings.Sandbox)
		}
		if len(values) > 0 {
			return strings.Join(values, "；")
		}
	}
	if value := agentproto.NormalizeAccessMode(event.AccessMode); value != "" {
		return agentproto.DisplayAccessModeShort(value)
	}
	return ""
}
