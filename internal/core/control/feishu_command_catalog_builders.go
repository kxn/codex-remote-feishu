package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func BuildFeishuCommandMenuHomePageViewForContext(ctx CatalogContext) FeishuPageView {
	ctx = NormalizeCatalogContext(ctx)
	return FeishuPageView{
		CommandID:      FeishuCommandMenu,
		CatalogBackend: ctx.Backend,
		Title:          "命令菜单",
		Interactive:    true,
		DisplayStyle:   CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    FeishuCommandBreadcrumbs("", ""),
		Sections: []CommandCatalogSection{{
			Title:   "",
			Entries: buildFeishuCommandMenuGroupEntries(ctx),
		}},
	}
}

func BuildFeishuCommandMenuPageViewForContext(view FeishuCatalogMenuView, ctx CatalogContext) FeishuPageView {
	ctx = NormalizeCatalogContext(ctx)
	groupID := strings.TrimSpace(view.GroupID)
	if groupID == "" {
		return BuildFeishuCommandMenuHomePageViewForContext(ctx)
	}
	stage := strings.TrimSpace(view.Stage)
	if stage == "" {
		stage = strings.TrimSpace(ctx.MenuStage)
	}
	if commandID, ok := ResolveFeishuCommandMenuGroupRootCommandID(ctx, groupID); ok {
		if root, ok := buildFeishuCommandMenuGroupRootPageView(commandID); ok {
			return root
		}
	}
	ctx.MenuStage = stage
	return BuildFeishuCommandMenuGroupPageViewForContext(groupID, ctx)
}

func BuildFeishuCommandMenuGroupPageViewForContext(groupID string, ctx CatalogContext) FeishuPageView {
	ctx = NormalizeCatalogContext(ctx)
	if _, ok := FeishuCommandGroupByID(groupID); !ok {
		return BuildFeishuCommandMenuHomePageViewForContext(ctx)
	}
	entries := make([]CommandCatalogEntry, 0, 6)
	for _, current := range ResolveFeishuCommandDisplayGroup(groupID, true, ctx) {
		entries = append(entries, buildFeishuCommandMenuEntryFromResolution(current, ctx.Backend))
	}
	return FeishuPageView{
		CommandID:      FeishuCommandMenu,
		CatalogBackend: ctx.Backend,
		Title:          "命令菜单",
		Interactive:    true,
		DisplayStyle:   CommandCatalogDisplayCompactButtons,
		Breadcrumbs:    FeishuCommandBreadcrumbs(groupID, ""),
		Sections: []CommandCatalogSection{{
			Title:   "",
			Entries: entries,
		}},
		RelatedButtons: []CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        CommandCatalogButtonAction,
			CommandText: FeishuCommandMenuCommandText(""),
		}},
	}
}

func BuildFeishuAttachmentRequiredPageView(def FeishuCommandDefinition, view FeishuCatalogConfigView) FeishuPageView {
	bodySections := BuildFeishuCommandConfigBodySections(def, view)
	noticeSections := BuildFeishuCommandConfigNoticeSections(def, view)
	sections := stampCommandSectionsCatalogProvenance([]CommandCatalogSection{{
		Title:   "开始 / 继续工作",
		Entries: buildFeishuRecoveryEntries(),
	}}, strings.TrimSpace(view.CatalogFamilyID), strings.TrimSpace(view.CatalogVariantID), view.CatalogBackend)
	return NormalizeFeishuPageView(FeishuPageView{
		CommandID:       strings.TrimSpace(def.ID),
		CatalogBackend:  view.CatalogBackend,
		Title:           strings.TrimSpace(def.Title),
		SummarySections: append([]FeishuCardTextSection(nil), bodySections...),
		BodySections:    append([]FeishuCardTextSection(nil), bodySections...),
		NoticeSections:  append([]FeishuCardTextSection(nil), noticeSections...),
		Interactive:     true,
		DisplayStyle:    CommandCatalogDisplayCompactButtons,
		Breadcrumbs:     FeishuCommandBreadcrumbs(def.GroupID, def.Title),
		Sections:        sections,
		RelatedButtons:  FeishuCommandBackButtons(def.GroupID),
	})
}

func FeishuCommandBreadcrumbs(groupID, title string) []CommandCatalogBreadcrumb {
	breadcrumbs := []CommandCatalogBreadcrumb{{Label: "菜单首页"}}
	if group, ok := FeishuCommandGroupByID(groupID); ok {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: group.Title})
	}
	if title = strings.TrimSpace(title); title != "" {
		breadcrumbs = append(breadcrumbs, CommandCatalogBreadcrumb{Label: title})
	}
	return breadcrumbs
}

func FeishuCommandBackButtons(groupID string) []CommandCatalogButton {
	if _, ok := FeishuCommandGroupByID(groupID); ok {
		return []CommandCatalogButton{{
			Label:       "返回上一层",
			Kind:        CommandCatalogButtonAction,
			CommandText: FeishuCommandMenuCommandText(groupID),
		}}
	}
	return nil
}

func FeishuCommandMenuCommandText(view string) string {
	if strings.TrimSpace(view) == "" {
		return "/menu"
	}
	return "/menu " + strings.TrimSpace(view)
}

func ResolveFeishuCommandMenuGroupRootCommandID(ctx CatalogContext, groupID string) (string, bool) {
	ctx = NormalizeCatalogContext(ctx)
	group, ok := FeishuCommandGroupByID(groupID)
	if !ok {
		return "", false
	}
	commandID := strings.TrimSpace(group.RootCommandID)
	if commandID == "" {
		return "", false
	}
	if _, ok := ResolveFeishuCommandDisplayFamily(commandID, true, ctx); !ok {
		return "", false
	}
	return commandID, true
}

func buildFeishuCommandMenuGroupEntries(ctx CatalogContext) []CommandCatalogEntry {
	ctx = NormalizeCatalogContext(ctx)
	entries := make([]CommandCatalogEntry, 0, len(FeishuCommandGroups()))
	for _, group := range FeishuCommandGroups() {
		if !feishuCommandMenuGroupVisibleInContext(group.ID, ctx) {
			continue
		}
		commandText := FeishuCommandMenuCommandText(group.ID)
		if commandID, ok := ResolveFeishuCommandMenuGroupRootCommandID(ctx, group.ID); ok {
			if def, ok := FeishuCommandDefinitionByID(commandID); ok {
				commandText = strings.TrimSpace(def.CanonicalSlash)
			}
		}
		entries = append(entries, CommandCatalogEntry{
			Title:       strings.TrimSpace(group.Title),
			Description: "",
			Buttons: []CommandCatalogButton{{
				Label:       feishuSubmenuButtonLabel(group.Title),
				Kind:        CommandCatalogButtonAction,
				CommandText: commandText,
			}},
		})
	}
	return entries
}

func feishuCommandMenuGroupVisibleInContext(groupID string, ctx CatalogContext) bool {
	if _, ok := ResolveFeishuCommandMenuGroupRootCommandID(ctx, groupID); ok {
		return true
	}
	return len(ResolveFeishuCommandDisplayGroup(groupID, true, ctx)) > 0
}

func buildFeishuCommandMenuGroupRootPageView(commandID string) (FeishuPageView, bool) {
	switch strings.TrimSpace(commandID) {
	case FeishuCommandAdmin:
		return BuildFeishuAdminRootPageView(true), true
	case FeishuCommandWorkspace:
		return BuildFeishuWorkspaceRootPageView(true), true
	default:
		return FeishuPageView{}, false
	}
}

func buildFeishuRecoveryEntries() []CommandCatalogEntry {
	return []CommandCatalogEntry{
		buildFeishuRecoveryEntry(FeishuCommandList),
		buildFeishuRecoveryEntry(FeishuCommandUse),
		buildFeishuRecoveryEntry(FeishuCommandStatus),
	}
}

func buildFeishuRecoveryEntry(commandID string) CommandCatalogEntry {
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		return CommandCatalogEntry{}
	}
	return buildFeishuCommandCatalogEntryWithCatalog(def, def.ID, defaultFeishuCommandDisplayVariantID(def.ID), "", feishuCommandMenuButtonLabel(def))
}

func buildFeishuCommandMenuEntry(def FeishuCommandDefinition) CommandCatalogEntry {
	return buildFeishuCommandCatalogEntryWithCatalog(def, def.ID, defaultFeishuCommandDisplayVariantID(def.ID), agentproto.BackendCodex, feishuCommandMenuButtonLabel(def))
}

func buildFeishuCommandMenuEntryFromResolution(resolution FeishuCommandDisplayResolution, backend agentproto.Backend) CommandCatalogEntry {
	return buildFeishuCommandCatalogEntryWithCatalog(
		resolution.Definition,
		resolution.FamilyID,
		resolution.VariantID,
		backend,
		feishuCommandMenuButtonLabel(resolution.Definition),
	)
}

func buildFeishuCommandCatalogEntry(def FeishuCommandDefinition, buttonLabel string) CommandCatalogEntry {
	return buildFeishuCommandCatalogEntryWithCatalog(def, def.ID, defaultFeishuCommandDisplayVariantID(def.ID), agentproto.BackendCodex, buttonLabel)
}

func buildFeishuCommandCatalogEntryWithCatalog(def FeishuCommandDefinition, familyID, variantID string, backend agentproto.Backend, buttonLabel string) CommandCatalogEntry {
	command := strings.TrimSpace(def.CanonicalSlash)
	entry := CommandCatalogEntry{
		Title:       strings.TrimSpace(def.Title),
		Description: strings.TrimSpace(def.Description),
		Examples:    append([]string(nil), def.Examples...),
	}
	if command != "" {
		entry.Commands = []string{command}
	}
	if buttonLabel = strings.TrimSpace(buttonLabel); buttonLabel != "" && command != "" {
		entry.Buttons = append(entry.Buttons, CommandCatalogButton{
			Label:            buttonLabel,
			Kind:             CommandCatalogButtonAction,
			CommandText:      command,
			CommandID:        strings.TrimSpace(def.ID),
			CatalogFamilyID:  strings.TrimSpace(familyID),
			CatalogVariantID: strings.TrimSpace(variantID),
			CatalogBackend:   agentproto.NormalizeBackend(backend),
		})
	}
	return entry
}

func stampCommandSectionsCatalogProvenance(sections []CommandCatalogSection, familyID, variantID string, backend agentproto.Backend) []CommandCatalogSection {
	if familyID == "" && variantID == "" && backend == "" {
		return sections
	}
	out := make([]CommandCatalogSection, 0, len(sections))
	for _, section := range sections {
		cloned := CommandCatalogSection{
			Title:   strings.TrimSpace(section.Title),
			Entries: make([]CommandCatalogEntry, 0, len(section.Entries)),
		}
		for _, entry := range section.Entries {
			clonedEntry := entry
			clonedEntry.Buttons = cloneCommandCatalogButtons(entry.Buttons)
			for i := range clonedEntry.Buttons {
				if clonedEntry.Buttons[i].CommandID == "" {
					clonedEntry.Buttons[i].CommandID = familyID
				}
				if clonedEntry.Buttons[i].CatalogFamilyID == "" {
					clonedEntry.Buttons[i].CatalogFamilyID = familyID
				}
				if clonedEntry.Buttons[i].CatalogVariantID == "" {
					clonedEntry.Buttons[i].CatalogVariantID = variantID
				}
				if backend != "" {
					clonedEntry.Buttons[i].CatalogBackend = backend
				}
			}
			clonedEntry.Form = cloneCommandCatalogForm(entry.Form)
			if clonedEntry.Form != nil {
				if clonedEntry.Form.CommandID == "" {
					clonedEntry.Form.CommandID = familyID
				}
				if clonedEntry.Form.CatalogFamilyID == "" {
					clonedEntry.Form.CatalogFamilyID = familyID
				}
				if clonedEntry.Form.CatalogVariantID == "" {
					clonedEntry.Form.CatalogVariantID = variantID
				}
				if backend != "" {
					clonedEntry.Form.CatalogBackend = backend
				}
			}
			cloned.Entries = append(cloned.Entries, clonedEntry)
		}
		out = append(out, cloned)
	}
	return out
}

func feishuCommandMenuButtonLabel(def FeishuCommandDefinition) string {
	title := strings.TrimSpace(def.Title)
	command := strings.TrimSpace(def.CanonicalSlash)
	switch {
	case title == "":
		return command
	case command == "":
		return title
	default:
		return title + " " + command
	}
}

func feishuSubmenuButtonLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "进入"
	}
	return label
}
