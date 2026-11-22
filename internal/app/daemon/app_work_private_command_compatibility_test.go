package daemon

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateSlashMenuPreservesExistingPageNames(t *testing.T) {
	for _, tt := range []struct{ text, page string }{
		{"/menu", "menu"},
		{"/menu current_work", "basic"},
		{"/menu send_settings", "settings"},
		{"/menu switch_target", "workspace"},
		{"/menu common_tools", "tools"},
		{"/menu maintenance", "admin"},
		{" /menu SEND_SETTINGS ", "settings"},
		{"/menu basic", "basic"},
		{"/menu settings", "settings"},
		{"/menu workspace", "workspace"},
		{"/menu tools", "tools"},
		{"/menu admin", "admin"},
	} {
		t.Run(tt.text, func(t *testing.T) {
			action, known := parseTeamPrivateCommand(tt.text)
			if !known || action.Kind != control.ActionShowCommandMenu {
				t.Fatalf("parse = %+v, %v", action, known)
			}
			page, err := teamPrivateCommandMenuPage(action.Text)
			if err != nil || page != tt.page {
				t.Fatalf("page = %q, %v; want %q", page, err, tt.page)
			}
		})
	}
	for _, text := range []string{"/menu typo", "/menu send_settings ignored"} {
		if page, err := teamPrivateCommandMenuPage(text); err == nil || page != "" {
			t.Fatalf("invalid menu %q silently opened %q: %v", text, page, err)
		}
	}
}

func TestPrivateSlashMenuQueuesRequestedSection(t *testing.T) {
	for _, tt := range []struct{ page, title string }{
		{"current_work", "基本命令"},
		{"switch_target", "工作区与会话"},
		{"common_tools", "常用工具"},
		{"maintenance", "系统管理"},
	} {
		t.Run(tt.page, func(t *testing.T) {
			a, source := teamPickerFixture(t)
			id, err := a.queueTeamPrivateCommandMenu(t.Context(), source, "/menu "+tt.page)
			if err != nil {
				t.Fatal(err)
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				view, err := tx.GetUIView(id)
				if err != nil {
					return err
				}
				body, err := tx.GetContent(source.ScopeID, view.BodyRef)
				if err == nil && !strings.Contains(string(body.Data), tt.title) {
					t.Fatalf("requested section missing: %s", body.Data)
				}
				return err
			})
		})
	}
}

func TestPrivateSendFileTextAndFormKeepPath(t *testing.T) {
	const path = "报告目录/final report.pdf"
	for _, text := range []string{"/sendfile " + path, " /sendfile\t" + path + " "} {
		action, known := parseTeamPrivateCommand(text)
		if !known || action.Kind != control.ActionSendFile || action.Text != strings.TrimSpace(text) {
			t.Fatalf("sendfile parse = %+v, %v", action, known)
		}
	}
	action, found, err := teamMenuToolCommand("private.file-send", map[string]any{"file_path": path})
	if err != nil || !found || action.Kind != control.ActionSendFile || action.Text != "/sendfile "+path {
		t.Fatalf("file form = %+v, %v, %v", action, found, err)
	}
	if _, known := parseTeamPrivateCommand("/sendfile-extra " + path); known {
		t.Fatal("unrelated command accepted as sendfile")
	}
}

func TestPrivateBareSendFileOpensFileForm(t *testing.T) {
	a, source := teamPickerFixture(t)
	action, known := parseTeamPrivateCommand("/sendfile")
	if !known {
		t.Fatal("bare sendfile not parsed")
	}
	id, err := a.handleTeamPrivateOperation(t.Context(), source, action)
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		body, err := tx.GetContent(source.ScopeID, view.BodyRef)
		if err == nil && !strings.Contains(string(body.Data), "private.file-send") {
			t.Fatalf("file form missing: %s", body.Data)
		}
		return err
	})
}
