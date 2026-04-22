package cronruntime

import (
	"fmt"
	"strings"
	"time"
)

type GatewayIdentity struct {
	GatewayID string
	AppID     string
}

type OwnerStatus string

const (
	OwnerStatusNone        OwnerStatus = "none"
	OwnerStatusHealthy     OwnerStatus = "healthy"
	OwnerStatusBootstrap   OwnerStatus = "bootstrap"
	OwnerStatusUnavailable OwnerStatus = "unavailable"
	OwnerStatusMismatch    OwnerStatus = "mismatch"
	OwnerStatusUnresolved  OwnerStatus = "unresolved"
)

type OwnerBinding struct {
	GatewayID string
	AppID     string
	BoundAt   time.Time
}

type OwnerResolution struct {
	Status       OwnerStatus
	State        *StateFile
	ScopeKey     string
	Label        string
	Binding      BitableState
	Gateway      GatewayIdentity
	PersistOwner *OwnerBinding
	CurrentOwner *OwnerBinding
	Message      string
}

type OwnerView struct {
	Status      OwnerStatus
	StatusLabel string
	Detail      string
	NextAction  string
	NeedsRepair bool
}

type OwnerResolveOptions struct {
	AllowCreate        bool
	CreateStateIfEmpty bool
}

func OwnerBindingBackfill(current *OwnerBinding, identity GatewayIdentity) (*OwnerBinding, bool) {
	if current == nil {
		return nil, false
	}
	next := *current
	changed := false
	if strings.TrimSpace(next.AppID) == "" && strings.TrimSpace(identity.AppID) != "" {
		next.AppID = strings.TrimSpace(identity.AppID)
		changed = true
	}
	if next.BoundAt.IsZero() {
		next.BoundAt = time.Now().UTC()
		changed = true
	}
	if !changed {
		return nil, false
	}
	return &next, true
}

func OwnerBindingFromState(stateValue *StateFile) *OwnerBinding {
	if stateValue == nil {
		return nil
	}
	gatewayID := strings.TrimSpace(stateValue.OwnerGatewayID)
	appID := strings.TrimSpace(stateValue.OwnerAppID)
	if gatewayID == "" && appID == "" {
		return nil
	}
	return &OwnerBinding{
		GatewayID: gatewayID,
		AppID:     appID,
		BoundAt:   stateValue.OwnerBoundAt.UTC(),
	}
}

func ApplyOwnerBinding(stateValue *StateFile, owner *OwnerBinding) {
	if stateValue == nil || owner == nil {
		return
	}
	stateValue.OwnerGatewayID = strings.TrimSpace(owner.GatewayID)
	stateValue.OwnerAppID = strings.TrimSpace(owner.AppID)
	stateValue.OwnerBoundAt = owner.BoundAt.UTC()
}

func OwnerActionError(action string, resolution OwnerResolution) error {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "执行 Cron 操作"
	}
	switch resolution.Status {
	case OwnerStatusHealthy, OwnerStatusBootstrap:
		return nil
	case OwnerStatusNone:
		return fmt.Errorf("尚未初始化 Cron 配置表，请先执行 `/cron repair`")
	case OwnerStatusUnavailable:
		return fmt.Errorf("%s失败：当前 Cron 绑定已失效，请先执行 `/cron repair`", action)
	case OwnerStatusMismatch:
		return fmt.Errorf("%s失败：当前 Cron 绑定与运行时配置不一致，请先执行 `/cron repair`", action)
	case OwnerStatusUnresolved:
		return fmt.Errorf("%s失败：当前 Cron 绑定待修复，请先执行 `/cron repair`", action)
	default:
		return fmt.Errorf("%s失败：Cron owner 状态无效", action)
	}
}

func (r OwnerResolution) WritebackTarget() WritebackTarget {
	if strings.TrimSpace(r.Binding.AppToken) == "" {
		return WritebackTarget{}
	}
	gatewayID := strings.TrimSpace(r.Gateway.GatewayID)
	if gatewayID == "" && r.CurrentOwner != nil {
		gatewayID = strings.TrimSpace(r.CurrentOwner.GatewayID)
	}
	return WritebackTarget{
		GatewayID: gatewayID,
		Bitable:   r.Binding,
	}
}
