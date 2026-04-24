package eventcontract

import "github.com/kxn/codex-remote-feishu/internal/core/handoffcontract"

type InlineReplaceMode string

const (
	InlineReplaceNone        InlineReplaceMode = ""
	InlineReplaceCurrentCard InlineReplaceMode = "current_card"
)

type VisibilityClass string

const (
	VisibilityClassDefault       VisibilityClass = ""
	VisibilityClassAlwaysVisible VisibilityClass = "always_visible"
	VisibilityClassProgressText  VisibilityClass = "progress_text"
	VisibilityClassPlan          VisibilityClass = "plan"
	VisibilityClassProcessDetail VisibilityClass = "process_detail"
	VisibilityClassUINavigation  VisibilityClass = "ui_navigation"
)

type HandoffClass = handoffcontract.HandoffClass

const (
	HandoffClassDefault         = handoffcontract.HandoffClassDefault
	HandoffClassNavigation      = handoffcontract.HandoffClassNavigation
	HandoffClassNotice          = handoffcontract.HandoffClassNotice
	HandoffClassThreadSelection = handoffcontract.HandoffClassThreadSelection
	HandoffClassProcessDetail   = handoffcontract.HandoffClassProcessDetail
	HandoffClassTerminalContent = handoffcontract.HandoffClassTerminalContent
)

type FirstResultDisposition string

const (
	FirstResultDispositionDefault FirstResultDisposition = ""
	FirstResultDispositionKeep    FirstResultDisposition = "keep"
	FirstResultDispositionDrop    FirstResultDisposition = "drop"
)

type OwnerCardDisposition string

const (
	OwnerCardDispositionDefault OwnerCardDisposition = ""
	OwnerCardDispositionKeep    OwnerCardDisposition = "keep"
	OwnerCardDispositionDrop    OwnerCardDisposition = "drop"
)

type DeliverySemantics struct {
	VisibilityClass        VisibilityClass
	HandoffClass           HandoffClass
	FirstResultDisposition FirstResultDisposition
	OwnerCardDisposition   OwnerCardDisposition
}

func (semantics DeliverySemantics) Normalized() DeliverySemantics {
	switch semantics.VisibilityClass {
	case VisibilityClassAlwaysVisible, VisibilityClassProgressText, VisibilityClassPlan, VisibilityClassProcessDetail, VisibilityClassUINavigation:
	default:
		semantics.VisibilityClass = VisibilityClassDefault
	}
	switch semantics.HandoffClass {
	case HandoffClassNavigation, HandoffClassNotice, HandoffClassThreadSelection, HandoffClassProcessDetail, HandoffClassTerminalContent:
	default:
		semantics.HandoffClass = HandoffClassDefault
	}
	switch semantics.FirstResultDisposition {
	case FirstResultDispositionKeep, FirstResultDispositionDrop:
	default:
		semantics.FirstResultDisposition = FirstResultDispositionDefault
	}
	switch semantics.OwnerCardDisposition {
	case OwnerCardDispositionKeep, OwnerCardDispositionDrop:
	default:
		semantics.OwnerCardDisposition = OwnerCardDispositionDefault
	}
	return semantics
}
