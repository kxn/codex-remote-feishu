package control

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildFeishuWorkspaceRootPageViewUsesCanonicalCommandDefinitions(t *testing.T) {
	page := BuildFeishuWorkspaceRootPageView(false)
	if got, want := pageEntryCommands(page), []string{
		canonicalSlashForTest(t, FeishuCommandWorkspaceList),
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewDir),
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewGit),
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewWorktree),
		canonicalSlashForTest(t, FeishuCommandWorkspaceDetach),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace root commands = %#v, want %#v", got, want)
	}
}

func TestBuildFeishuWorkspaceNewPageViewUsesCanonicalCommandDefinitions(t *testing.T) {
	page := BuildFeishuWorkspaceNewPageView(false)
	if got, want := pageEntryCommands(page), []string{
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewDir),
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewGit),
		canonicalSlashForTest(t, FeishuCommandWorkspaceNewWorktree),
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace new commands = %#v, want %#v", got, want)
	}
	if len(page.RelatedButtons) != 1 {
		t.Fatalf("workspace new related buttons = %d, want 1", len(page.RelatedButtons))
	}
	if got, want := strings.TrimSpace(page.RelatedButtons[0].CommandText), canonicalSlashForTest(t, FeishuCommandWorkspace); got != want {
		t.Fatalf("workspace new back command = %q, want %q", got, want)
	}
}

func canonicalSlashForTest(t *testing.T, commandID string) string {
	t.Helper()
	def, ok := FeishuCommandDefinitionByID(commandID)
	if !ok {
		t.Fatalf("missing command definition for %s", commandID)
	}
	return strings.TrimSpace(def.CanonicalSlash)
}

func pageEntryCommands(page FeishuPageView) []string {
	var commands []string
	for _, section := range page.Sections {
		for _, entry := range section.Entries {
			if len(entry.Buttons) == 0 {
				continue
			}
			commands = append(commands, strings.TrimSpace(entry.Buttons[0].CommandText))
		}
	}
	return commands
}
