package owneridentity

import (
	"strings"
	"time"
)

// OwnerCardVerdict 是 owner 卡片校验的判定结果。
type OwnerCardVerdict string

const (
	// OwnerCardOK 表示卡片有效且操作者获授权。
	OwnerCardOK OwnerCardVerdict = "ok"
	// OwnerCardExpired 表示卡片缺失、已过期或 flowID 不匹配。
	OwnerCardExpired OwnerCardVerdict = "expired"
	// OwnerCardWrongSurface 表示卡片绑定到其他 surface。
	OwnerCardWrongSurface OwnerCardVerdict = "wrong_surface"
	// OwnerCardUnauthorized 表示操作者不是卡片发起者。
	OwnerCardUnauthorized OwnerCardVerdict = "unauthorized"
)

// OwnerCardClaims 是 owner 卡片校验所需的全部事实。
type OwnerCardClaims struct {
	FlowID           string
	SurfaceSessionID string
	OwnerUserID      string
	ExpiresAt        time.Time
}

// VerifyOwnerCard 是 owner 卡片判定的唯一实现：按 flowID 匹配 → 未过期 →
// surface 匹配 → owner 决策的顺序返回判定结果。所有调用方（core 通用卡片、
// daemon upgrade / codex_upgrade / turn_patch）必须委托本函数，禁止复制
// 判定序列。错误码/文案/事件构造与过期清理（状态变更）由调用方负责。
//
// SurfaceSessionID 为空表示不校验 surface（flow 按 surface 存储的场景）。
func VerifyOwnerCard(claims OwnerCardClaims, surfaceID, flowID, actorUserID, surfaceActorUserID string, now time.Time) OwnerCardVerdict {
	if strings.TrimSpace(flowID) == "" || strings.TrimSpace(claims.FlowID) != strings.TrimSpace(flowID) {
		return OwnerCardExpired
	}
	if !claims.ExpiresAt.IsZero() && !claims.ExpiresAt.After(now) {
		return OwnerCardExpired
	}
	if strings.TrimSpace(claims.SurfaceSessionID) != "" && strings.TrimSpace(claims.SurfaceSessionID) != strings.TrimSpace(surfaceID) {
		return OwnerCardWrongSurface
	}
	if Decide(claims.OwnerUserID, actorUserID, surfaceActorUserID) != DecisionAllow {
		return OwnerCardUnauthorized
	}
	return OwnerCardOK
}
