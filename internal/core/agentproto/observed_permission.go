package agentproto

import "strings"

type ObservedPermissionProjectionKind string

const (
	ObservedPermissionProjectionKindExact    ObservedPermissionProjectionKind = "exact"
	ObservedPermissionProjectionKindPartial  ObservedPermissionProjectionKind = "partial"
	ObservedPermissionProjectionKindUnmapped ObservedPermissionProjectionKind = "unmapped"
)

type ObservedPermissionState struct {
	NativeMode          string                           `json:"nativeMode,omitempty"`
	ProjectedAccessMode string                           `json:"projectedAccessMode,omitempty"`
	ProjectedPlanMode   string                           `json:"projectedPlanMode,omitempty"`
	ProjectionKind      ObservedPermissionProjectionKind `json:"projectionKind,omitempty"`
}

func NormalizeObservedPermissionProjectionKind(value ObservedPermissionProjectionKind) ObservedPermissionProjectionKind {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case "exact":
		return ObservedPermissionProjectionKindExact
	case "partial":
		return ObservedPermissionProjectionKindPartial
	case "unmapped":
		return ObservedPermissionProjectionKindUnmapped
	default:
		return ""
	}
}

func CloneObservedPermissionState(value *ObservedPermissionState) *ObservedPermissionState {
	if value == nil {
		return nil
	}
	copy := *value
	copy.NativeMode = strings.TrimSpace(copy.NativeMode)
	copy.ProjectedAccessMode = NormalizeAccessMode(copy.ProjectedAccessMode)
	copy.ProjectedPlanMode = strings.TrimSpace(copy.ProjectedPlanMode)
	copy.ProjectionKind = NormalizeObservedPermissionProjectionKind(copy.ProjectionKind)
	if copy.NativeMode == "" && copy.ProjectedAccessMode == "" && copy.ProjectedPlanMode == "" && copy.ProjectionKind == "" {
		return nil
	}
	return &copy
}
