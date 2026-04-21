package control

import (
	"strings"
	"testing"
)

func TestStaticCommandCatalogsUsePlainTextContracts(t *testing.T) {
	cases := []struct {
		name    string
		catalog FeishuDirectCommandCatalog
	}{
		{name: "help", catalog: FeishuCommandHelpCatalog()},
		{name: "menu", catalog: FeishuCommandMenuCatalog()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertCommandCatalogUsesPlainTextContracts(t, tc.catalog)
		})
	}
}

func TestDisplayCatalogBuilderUsesPlainTextContracts(t *testing.T) {
	catalog := BuildFeishuCommandCatalogForDisplay(
		"Slash 命令帮助",
		"当前展示 canonical 命令。",
		false,
		"normal",
		"",
	)
	assertCommandCatalogUsesPlainTextContracts(t, catalog)
}

func TestCommandViewCatalogBuildersUsePlainTextContracts(t *testing.T) {
	t.Run("menu_home", func(t *testing.T) {
		assertCommandCatalogUsesPlainTextContracts(t, BuildFeishuCommandMenuHomeCatalog())
	})

	t.Run("menu_group", func(t *testing.T) {
		assertCommandCatalogUsesPlainTextContracts(t, BuildFeishuCommandMenuGroupCatalog("current_work", "normal", "normal_working"))
	})

	t.Run("attachment_required", func(t *testing.T) {
		def, ok := FeishuCommandDefinitionByID(FeishuCommandReasoning)
		if !ok {
			t.Fatalf("expected builtin command definition")
		}
		catalog := BuildFeishuAttachmentRequiredCatalog(def, FeishuCommandConfigView{
			CommandID:          def.ID,
			RequiresAttachment: true,
		})
		if len(catalog.SummarySections) == 0 {
			t.Fatalf("expected attachment-required catalog to expose summary sections: %#v", catalog)
		}
		assertCommandCatalogUsesPlainTextContracts(t, catalog)
	})
}

func assertCommandCatalogUsesPlainTextContracts(t *testing.T, catalog FeishuDirectCommandCatalog) {
	t.Helper()
	assertCatalogTextAvoidsFeishuMarkdown(t, "summary", catalog.Summary)
	for _, section := range catalog.SummarySections {
		assertCardTextSectionUsesPlainText(t, section)
	}
	for _, section := range catalog.BodySections {
		assertCardTextSectionUsesPlainText(t, section)
	}
	for _, section := range catalog.NoticeSections {
		assertCardTextSectionUsesPlainText(t, section)
	}
	for _, section := range catalog.Sections {
		for _, entry := range section.Entries {
			assertCatalogTextAvoidsFeishuMarkdown(t, "entry title", entry.Title)
			assertCatalogTextAvoidsFeishuMarkdown(t, "entry description", entry.Description)
			for _, command := range entry.Commands {
				assertCatalogTextAvoidsFeishuMarkdown(t, "entry command", command)
			}
			for _, example := range entry.Examples {
				assertCatalogTextAvoidsFeishuMarkdown(t, "entry example", example)
			}
		}
	}
}

func assertCardTextSectionUsesPlainText(t *testing.T, section FeishuCardTextSection) {
	t.Helper()
	normalized := section.Normalized()
	assertCatalogTextAvoidsFeishuMarkdown(t, "summary section label", normalized.Label)
	for _, line := range normalized.Lines {
		assertCatalogTextAvoidsFeishuMarkdown(t, "summary section line", line)
	}
}

func assertCatalogTextAvoidsFeishuMarkdown(t *testing.T, label, text string) {
	t.Helper()
	if strings.Contains(strings.TrimSpace(text), "<text_tag") {
		t.Fatalf("expected %s to stay in plain-text fields, got %q", label, text)
	}
}
