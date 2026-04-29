package control

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// FeishuCatalogView is the UI-owned view payload for interactive command menu
// and config cards. It carries semantic state; the final Feishu card layout is
// still projected separately during the transition.
type FeishuCatalogView struct {
	Menu   *FeishuCatalogMenuView
	Config *FeishuCatalogConfigView
	Page   *FeishuPageView
}

type FeishuCatalogMenuView struct {
	Stage   string
	GroupID string
}

type FeishuCatalogConfigView struct {
	CatalogFamilyID    string
	CatalogVariantID   string
	CatalogBackend     agentproto.Backend
	CommandID          string
	RequiresAttachment bool
	CurrentValue       string
	EffectiveValue     string
	OverrideValue      string
	OverrideExtraValue string
	FormDefaultValue   string
	FormOptions        []CommandCatalogFormFieldOption
	StatusKind         string
	StatusText         string
	Sealed             bool
}
