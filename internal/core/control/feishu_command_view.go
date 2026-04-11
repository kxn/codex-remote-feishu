package control

// FeishuCommandView is the UI-owned view payload for interactive command menu
// and config cards. It carries semantic state; the final Feishu card layout is
// still projected separately during the transition.
type FeishuCommandView struct {
	Menu   *FeishuCommandMenuView
	Config *FeishuCommandConfigView
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
}
