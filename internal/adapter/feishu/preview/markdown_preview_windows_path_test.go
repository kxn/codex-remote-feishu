package preview

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestDriveMarkdownPreviewerRewritesWrappedFileReferencesWithSpacesAndParens(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "My Report.md"), "# report\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "report (final).md"), "# final\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "angle path.md"), "# angle\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open [report](docs/My Report.md), `docs/report (final).md`, and <docs/angle path.md>.",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "Open [report](https://preview/file-1), [docs/report (final).md](https://preview/file-2), and [docs/angle path.md](https://preview/file-3)."
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 3 {
		t.Fatalf("expected all wrapped references to be materialized, got %#v", api.uploadFileCalls)
	}
	if !strings.HasPrefix(api.uploadFileCalls[0].FileName, "My Report--") ||
		!strings.HasPrefix(api.uploadFileCalls[1].FileName, "report (final)--") ||
		!strings.HasPrefix(api.uploadFileCalls[2].FileName, "angle path--") {
		t.Fatalf("unexpected uploaded files: %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerKeepsBareSpacePathAsBoundary(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "My Report.md"), "# report\n")
	writeMarkdownFile(t, filepath.Join(root, "docs", "plain.md"), "# plain\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open docs/My Report.md and docs/plain.md.",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "Open docs/My Report.md and [docs/plain.md](https://preview/file-1)."
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 || !strings.HasPrefix(api.uploadFileCalls[0].FileName, "plain--") {
		t.Fatalf("expected only bare path without spaces to be materialized, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerKeepsCommandLikeInlineCodeWithSpacePath(t *testing.T) {
	root := t.TempDir()
	writeMarkdownFile(t, filepath.Join(root, "docs", "My Report.md"), "# report\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Keep `cat docs/My Report.md | head -n 5`, open [report](docs/My Report.md).",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "Keep `cat docs/My Report.md | head -n 5`, open [report](https://preview/file-1)."
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 || !strings.HasPrefix(api.uploadFileCalls[0].FileName, "My Report--") {
		t.Fatalf("expected only explicit markdown link to be materialized, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerRewritesCodeFileLocationWithSpacesToWebPreviewLink(t *testing.T) {
	root := t.TempDir()
	writePreviewFile(t, filepath.Join(root, "docs", "My Code.go"), "package docs\n\nfunc Demo() {}\n")
	previewer := NewDriveMarkdownPreviewer(nil, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	web := &fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"}
	previewer.SetWebPreviewPublisher(web)

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "Open `docs/My Code.go:3`.",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	if !strings.Contains(result.Block.Text, "[docs/My Code.go:3](https://preview.example/g/shared/") {
		t.Fatalf("expected web preview link for code file location with spaces, got %q", result.Block.Text)
	}
	if !strings.Contains(result.Block.Text, "loc=L3") || !strings.Contains(result.Block.Text, "#L3") {
		t.Fatalf("expected location to be carried into preview url, got %q", result.Block.Text)
	}
	if len(web.issuedFor) != 1 {
		t.Fatalf("expected one web preview grant request, got %#v", web.issuedFor)
	}
}

func TestParseStandalonePreviewReferenceWholeSupportsWindowsPathWithSpaces(t *testing.T) {
	target := `C:\Users\me\My Documents\note.md:12`

	rawTarget, display, ok := parseStandalonePreviewReferenceWhole(target)

	if !ok {
		t.Fatalf("expected whole Windows path with spaces to parse")
	}
	if rawTarget != target || display != target {
		t.Fatalf("unexpected parsed target/display: raw=%q display=%q", rawTarget, display)
	}
	base, location, suffix := splitPreviewLocationSuffix(rawTarget)
	if base != `C:\Users\me\My Documents\note.md` || location.Line != 12 || suffix != ":12" {
		t.Fatalf("unexpected location split: base=%q location=%#v suffix=%q", base, location, suffix)
	}
}

func TestPreviewPathCandidatesTreatUNCPathAsAbsolute(t *testing.T) {
	roots := []string{`D:\Work\GoDot\interview-simulator`}
	target := `\\server\share\docs\characters.md`

	candidates := previewPathCandidates(target, roots)

	if len(candidates) != 1 || candidates[0] != target {
		t.Fatalf("expected UNC path to bypass root join, got %#v", candidates)
	}
}
