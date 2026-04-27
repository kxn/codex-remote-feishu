package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

// FeishuPageView is the generic page-card DTO used by menu/config/root pages.
// It intentionally avoids command-specific implicit defaults.
type FeishuPageView struct {
	PageID                        string
	CommandID                     string
	CatalogBackend                agentproto.Backend
	Title                         string
	DetourLabel                   string
	MessageID                     string
	TrackingKey                   string
	ThemeKey                      string
	Patchable                     bool
	Breadcrumbs                   []CommandCatalogBreadcrumb
	SummarySections               []FeishuCardTextSection
	BodySections                  []FeishuCardTextSection
	NoticeSections                []FeishuCardTextSection
	StatusKind                    string
	StatusText                    string
	Phase                         frontstagecontract.Phase
	ActionPolicy                  frontstagecontract.ActionPolicy
	Interactive                   bool
	Sealed                        bool
	DisplayStyle                  CommandCatalogDisplayStyle
	Sections                      []CommandCatalogSection
	RelatedButtons                []CommandCatalogButton
	SuppressDefaultRelatedButtons bool
}

func NormalizeFeishuPageView(view FeishuPageView) FeishuPageView {
	commandID := strings.TrimSpace(view.CommandID)
	def, _ := FeishuCommandDefinitionByID(commandID)
	allowCommandGroupFallback := commandID != FeishuCommandMenu
	title := strings.TrimSpace(view.Title)
	if title == "" {
		title = strings.TrimSpace(def.Title)
	}
	displayStyle := view.DisplayStyle
	if displayStyle == "" {
		displayStyle = CommandCatalogDisplayCompactButtons
	}
	breadcrumbs := cloneCommandBreadcrumbs(view.Breadcrumbs)
	if allowCommandGroupFallback && len(breadcrumbs) == 0 && strings.TrimSpace(def.GroupID) != "" {
		breadcrumbs = FeishuCommandBreadcrumbs(def.GroupID, title)
	}
	bodySections := BuildFeishuPageBodySections(view)
	noticeSections := BuildFeishuPageNoticeSections(view)
	sections := cloneCommandCatalogSections(view.Sections)
	relatedButtons := cloneCommandCatalogButtons(view.RelatedButtons)
	actionPolicy := view.ActionPolicy
	if actionPolicy == "" && !view.Sealed && !view.Interactive {
		actionPolicy = frontstagecontract.ActionPolicyReadOnly
	}
	frame := frontstagecontract.NormalizeFrame(frontstagecontract.Frame{
		OwnerKind:    frontstagecontract.OwnerCardPage,
		Phase:        normalizePagePhase(view),
		ActionPolicy: actionPolicy,
	})
	interactive := frontstagecontract.AllowsPrimaryInput(frame.ActionPolicy)
	sealed := frontstagecontract.SealedForPhase(frame.Phase)
	if sealed {
		interactive = false
	} else if allowCommandGroupFallback && len(relatedButtons) == 0 && strings.TrimSpace(def.GroupID) != "" && !view.SuppressDefaultRelatedButtons {
		relatedButtons = FeishuCommandBackButtons(def.GroupID)
	}
	return FeishuPageView{
		PageID:                        strings.TrimSpace(view.PageID),
		CommandID:                     commandID,
		CatalogBackend:                agentproto.NormalizeBackend(view.CatalogBackend),
		Title:                         title,
		DetourLabel:                   strings.TrimSpace(view.DetourLabel),
		MessageID:                     strings.TrimSpace(view.MessageID),
		TrackingKey:                   strings.TrimSpace(view.TrackingKey),
		ThemeKey:                      strings.TrimSpace(view.ThemeKey),
		Patchable:                     view.Patchable,
		Breadcrumbs:                   breadcrumbs,
		SummarySections:               cloneNormalizedFeishuCardSections(bodySections),
		BodySections:                  bodySections,
		NoticeSections:                noticeSections,
		StatusKind:                    "",
		StatusText:                    "",
		Phase:                         frame.Phase,
		ActionPolicy:                  frame.ActionPolicy,
		Interactive:                   interactive,
		Sealed:                        sealed,
		DisplayStyle:                  displayStyle,
		Sections:                      sections,
		RelatedButtons:                relatedButtons,
		SuppressDefaultRelatedButtons: view.SuppressDefaultRelatedButtons,
	}
}

func normalizePagePhase(view FeishuPageView) frontstagecontract.Phase {
	if view.Phase != "" {
		return view.Phase
	}
	if view.Sealed {
		if strings.TrimSpace(view.StatusKind) == "error" {
			return frontstagecontract.PhaseFailed
		}
		return frontstagecontract.PhaseSucceeded
	}
	return frontstagecontract.PhaseEditing
}

func BuildFeishuPageBodySections(view FeishuPageView) []FeishuCardTextSection {
	return cloneNormalizedFeishuCardSections(firstNonEmptyFeishuCardSections(view.BodySections, view.SummarySections))
}

func BuildFeishuPageNoticeSections(view FeishuPageView) []FeishuCardTextSection {
	sections := make([]FeishuCardTextSection, 0, len(view.NoticeSections)+1)
	if feedback, ok := pageFeedbackSection(view.StatusKind, view.StatusText); ok {
		sections = append(sections, feedback)
	}
	sections = append(sections, cloneNormalizedFeishuCardSections(view.NoticeSections)...)
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func pageFeedbackSection(statusKind, statusText string) (FeishuCardTextSection, bool) {
	text := normalizeCommandFeedbackText(statusText)
	if text == "" {
		return FeishuCardTextSection{}, false
	}
	label := "状态"
	switch strings.TrimSpace(statusKind) {
	case "error":
		label = "错误"
	case "info":
		label = "说明"
	}
	return FeishuCardTextSection{
		Label: label,
		Lines: []string{text},
	}, true
}

func FeishuPageViewFromCommandPageView(view FeishuPageView) FeishuPageView {
	return NormalizeFeishuPageView(FeishuPageView{
		PageID:                        strings.TrimSpace(view.CommandID),
		CommandID:                     strings.TrimSpace(view.CommandID),
		CatalogBackend:                agentproto.NormalizeBackend(view.CatalogBackend),
		Title:                         strings.TrimSpace(view.Title),
		DetourLabel:                   strings.TrimSpace(view.DetourLabel),
		MessageID:                     strings.TrimSpace(view.MessageID),
		TrackingKey:                   strings.TrimSpace(view.TrackingKey),
		ThemeKey:                      strings.TrimSpace(view.ThemeKey),
		Patchable:                     view.Patchable,
		Breadcrumbs:                   cloneCommandBreadcrumbs(view.Breadcrumbs),
		SummarySections:               cloneNormalizedFeishuCardSections(view.SummarySections),
		BodySections:                  cloneNormalizedFeishuCardSections(view.BodySections),
		NoticeSections:                cloneNormalizedFeishuCardSections(view.NoticeSections),
		StatusKind:                    strings.TrimSpace(view.StatusKind),
		StatusText:                    strings.TrimSpace(view.StatusText),
		Phase:                         view.Phase,
		ActionPolicy:                  view.ActionPolicy,
		Interactive:                   view.Interactive,
		Sealed:                        view.Sealed,
		DisplayStyle:                  view.DisplayStyle,
		Sections:                      cloneCommandCatalogSections(view.Sections),
		RelatedButtons:                cloneCommandCatalogButtons(view.RelatedButtons),
		SuppressDefaultRelatedButtons: view.SuppressDefaultRelatedButtons,
	})
}
