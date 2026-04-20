package control

// FeishuCommandView is the UI-owned view payload for interactive command menu
// and config cards. It carries semantic state; the final Feishu card layout is
// still projected separately during the transition.
type FeishuCommandView struct {
	Menu   *FeishuCommandMenuView
	Config *FeishuCommandConfigView
	Page   *FeishuCommandPageView
}

type FeishuCommandMenuView struct {
	Stage   string
	GroupID string
}

type FeishuCommandConfigView struct {
	CommandID          string
	RequiresAttachment bool
	CurrentValue       string
	EffectiveValue     string
	OverrideValue      string
	OverrideExtraValue string
	FormDefaultValue   string
	StatusKind         string
	StatusText         string
	Sealed             bool
}

type FeishuCommandPageView struct {
	CommandID       string
	Title           string
	MessageID       string
	TrackingKey     string
	ThemeKey        string
	Patchable       bool
	Breadcrumbs     []CommandCatalogBreadcrumb
	SummarySections []FeishuCardTextSection
	StatusKind      string
	StatusText      string
	Interactive     bool
	DisplayStyle    CommandCatalogDisplayStyle
	Sections        []CommandCatalogSection
	RelatedButtons  []CommandCatalogButton
}
