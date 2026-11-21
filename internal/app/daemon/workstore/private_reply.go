package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A quoted private reply belongs to its actual, durably observed source, never
// to whichever navigation happens to be selected when the queue is drained.
// The navigation itself is not changed. An unresolved anchor is held for an
// explicit user-facing notice instead of silently delivering to another front.
func (t *Tx) privateReplyNavigation(record work.IngressRecord, nav work.PrivateNavigation) (work.PrivateNavigation, string, error) {
	anchor := record.Envelope.ParentID
	if anchor == "" {
		anchor = record.Envelope.RootID
	}
	if anchor == "" {
		anchor = record.Envelope.ThreadID
	}
	if anchor == "" {
		return nav, "", nil
	}
	const unresolved = "无法确认这条引用原来属于哪项工作。这条回复已保存，但尚未交给 Agent；请打开相应工作的历史并继续，或在当前工作中重新发送完整要求。"
	mapping, err := t.GetPlatformMessageMapping(record.Envelope.Tenant, record.Envelope.ChatID, anchor)
	if errors.Is(err, ErrNotFound) {
		return nav, unresolved, nil
	}
	if err != nil {
		return nav, "", err
	}
	if mapping.ScopeID != nav.PersonalScopeID {
		return nav, unresolved, nil
	}
	recalled, err := t.IsPlatformMessageRecalled(mapping.Tenant, mapping.ChatID, mapping.MessageID)
	if err != nil {
		return nav, "", err
	}
	if recalled {
		return nav, unresolved, nil
	}
	contextID := ""
	if mapping.IngressID != "" {
		source, e := t.GetIngress(mapping.IngressID)
		if e != nil {
			return nav, "", e
		}
		if source.Route.ScopeID != nav.PersonalScopeID || !samePrivateGatewayAuthority(source.Gateway, record.Gateway) || source.Route.ReplyRoutingError != "" {
			return nav, unresolved, nil
		}
		contextID = source.Route.ContextID
	} else {
		if mapping.RoleID != record.Gateway.RoleID || mapping.BotGeneration != record.Gateway.BotGeneration {
			return nav, unresolved, nil
		}
		op, e := t.GetOperation(mapping.OperationID)
		if e != nil {
			return nav, "", e
		}
		if op.ScopeID != nav.PersonalScopeID || op.Kind != "feishu.publish" || op.TargetID != record.Envelope.ChatID {
			return nav, unresolved, nil
		}
		var proven bool
		contextID, proven, e = t.privateReplyPublicationContext(op, record)
		if e != nil {
			return nav, "", e
		}
		if !proven {
			return nav, unresolved, nil
		}
	}
	if contextID == "" {
		return nav, unresolved, nil
	}
	front, err := t.GetContext(contextID)
	if err != nil {
		return nav, "", err
	}
	if front.Purpose == work.PurposeReview && front.Review != nil {
		front, err = t.GetContext(front.Review.SourceContextID)
		if err != nil {
			return nav, "", err
		}
	}
	if front.ScopeID != nav.PersonalScopeID || front.RoleID != nav.RoleID || front.Purpose == work.PurposeControl || front.Purpose == work.PurposeReview {
		return nav, unresolved, nil
	}
	if front.ProjectViewScopeID != "" {
		if e := t.RequireProjectPrincipalAccess(front.ProjectViewScopeID, nav.PrincipalID, record.Envelope.ReceivedAt); e != nil {
			if errors.Is(e, ErrScopeDenied) || errors.Is(e, ErrNotFound) {
				return nav, "这条引用属于之前的项目，但当前无法确认你的项目访问资格。这条回复已保存，尚未交给 Agent；请先重新进入该项目。", nil
			}
			return nav, "", e
		}
	}
	nav.FrontContextID, nav.ProjectScopeID, nav.ProjectContextID = front.ID, front.ProjectViewScopeID, front.ProjectContextID
	return nav, "", nil
}

func samePrivateGatewayAuthority(a, b work.IngressGateway) bool {
	return a.GatewayRef == b.GatewayRef && a.AppID == b.AppID && a.RoleID == b.RoleID && a.BotGeneration == b.BotGeneration
}
