package daemon

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A pre-async /admin web request may have created a tunnel or signed a URL.
// Its original operation remains authoritative across an application upgrade.
func (a *App) legacySystemWeb(ctx context.Context, source teamHumanSource, text string) (workui.Panel, bool, error) {
	var panel workui.Panel
	found := false
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := a.validateTeamPickerSource(tx, source); err != nil {
			return err
		}
		op, err := tx.GetOperation(teamID("human-host", source.Key+":host.admin.web"))
		if err == workstore.ErrNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		raw, err := json.Marshal(struct {
			Text   string
			Source workstore.PrivateActionSource
		}{text, source.Authority})
		if err != nil {
			return err
		}
		request := work.NewContent("expected", source.ScopeID, "application/json", raw, time.Now().UTC())
		if op.Kind != "host.admin.web" || op.ScopeID != source.ScopeID || op.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}) || op.RequestHash != request.SHA256 {
			return workstore.ErrIdempotencyConflict
		}
		panel = legacySystemWebPanel(op)
		return nil
	})
	return panel, found, err
}

func legacySystemWebPanel(op work.Operation) workui.Panel {
	panel := workui.Panel{Title: "管理页"}
	if op.State == work.OperationConfirmed {
		panel = decodeSystemPanel(op.ExternalReceipt, "管理页")
		if len(panel.Buttons) == 0 {
			for _, part := range strings.Fields(op.ExternalReceipt) {
				link := strings.TrimPrefix(strings.TrimPrefix(part, "临时管理页："), "本机管理页：")
				if parsed, err := url.Parse(link); err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
					panel.Buttons = append(panel.Buttons, workui.Button{Label: "打开已生成的管理页", URL: link})
					break
				}
			}
		}
	} else {
		panel.Paragraphs = []string{"既有管理页链接请求的结果尚未确认。可能已创建通道或链接；不会重复执行这次请求。"}
		if op.LastError != "" {
			panel.Paragraphs = append(panel.Paragraphs, op.LastError)
		}
	}
	panel.Buttons = append(panel.Buttons, workui.Button{Label: "核对既有请求状态", Action: "system.web-status", Values: map[string]any{"job_id": op.ID}}, systemCommandButton("重新生成管理页链接（再次操作）", "/admin web"))
	return panel
}
