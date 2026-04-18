package feishu

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestDriveMarkdownPreviewerRehydratesNeutralizedPathLabelLink(t *testing.T) {
	root := t.TempDir()
	absPath := writePreviewFile(t, filepath.Join(root, "internal", "core", "control", "types.go"), "package control\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
		CacheDir:   filepath.Join(root, "preview-cache"),
	})
	previewer.SetWebPreviewPublisher(&fakeWebPreviewPublisher{baseURL: "https://preview.example/g/shared/?t=token"})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "请查看 internal/core/control/types.go (`" + absPath + "`)。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	wantPrefix := "请查看 [internal/core/control/types.go](https://preview.example/g/shared/"
	if !strings.HasPrefix(result.Block.Text, wantPrefix) || !strings.HasSuffix(result.Block.Text, ")。") {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 0 {
		t.Fatalf("expected web preview path rewrite without drive upload, got %#v", api.uploadFileCalls)
	}
}

func TestDriveMarkdownPreviewerRehydratesNeutralizedSimpleLabelLink(t *testing.T) {
	root := t.TempDir()
	absPath := writePreviewFile(t, filepath.Join(root, "docs", "guide.md"), "# guide\n")
	api := newFakePreviewAPI()
	previewer := NewDriveMarkdownPreviewer(api, MarkdownPreviewConfig{
		ProcessCWD: root,
	})

	result, err := previewer.RewriteFinalBlock(context.Background(), MarkdownPreviewRequest{
		SurfaceSessionID: "feishu:user:ou_user",
		ActorUserID:      "ou_user",
		WorkspaceRoot:    root,
		ThreadCWD:        root,
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "先看 Guide (`" + absPath + "`)，再继续。",
		},
	})
	if err != nil {
		t.Fatalf("rewrite returned error: %v", err)
	}
	want := "先看 [Guide](https://preview/file-1)，再继续。"
	if result.Block.Text != want {
		t.Fatalf("unexpected rewritten text: %q", result.Block.Text)
	}
	if len(api.uploadFileCalls) != 1 {
		t.Fatalf("expected one materialized preview link, got %#v", api.uploadFileCalls)
	}
}
