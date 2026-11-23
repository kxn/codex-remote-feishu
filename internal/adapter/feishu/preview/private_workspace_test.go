package preview

import (
	"path/filepath"
	"testing"
)

func TestManagedPrivatePreviewUsesOnlyFrozenWorkspace(t *testing.T) {
	workspace := t.TempDir()
	daemonDir := t.TempDir()
	privatePath := writeMarkdownFile(t, filepath.Join(workspace, "report.md"), "private report")
	otherPath := writeMarkdownFile(t, filepath.Join(daemonDir, "other.md"), "unrelated report")
	p := NewDriveMarkdownPreviewer(newFakePreviewAPI(), MarkdownPreviewConfig{ProcessCWD: daemonDir})
	req := FinalBlockPreviewRequest{WorkspaceRoot: workspace, ThreadCWD: workspace, PrivateWorkspaceOnly: true}
	if path, ok, err := p.resolvePreviewPath(privatePath, req); err != nil || !ok || path != privatePath {
		t.Fatalf("workspace preview = %q, %v, %v", path, ok, err)
	}
	if path, ok, err := p.resolvePreviewPath(otherPath, req); err != nil || ok || path != "" {
		t.Fatalf("daemon path included in managed private preview: %q, %v, %v", path, ok, err)
	}
	if _, _, ok := p.loadTurnDiffAnchorText(turnDiffParsedFile{NewPath: otherPath}, req); ok {
		t.Fatal("diff context included daemon file outside the workspace")
	}
}
