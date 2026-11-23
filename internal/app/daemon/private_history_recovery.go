package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// privateHistoryRecovery handles only an explicit request from the owner. It
// does not turn a previously claimed source input into a new model request.
func (r privateHostReducer) privateHistoryRecovery(app *privatechat.App, action control.Action) ([]eventcontract.Event, bool, error) {
	if action.Kind != control.ActionTextMessage {
		return nil, false, nil
	}
	fields := strings.Fields(action.Text)
	if len(fields) == 0 || fields[0] != "/recover" {
		return nil, false, nil
	}
	if r.host.applications == nil || r.host.applications.journal == nil {
		return nil, true, privatechat.ErrDeferred
	}
	owner, generation, ok := app.OwnerGeneration(action.SurfaceSessionID)
	if !ok || owner.GatewayID != action.GatewayID || owner.ChatID != action.ChatID || owner.UserID != action.ActorUserID {
		return nil, true, privatechat.ErrInvalidOwner
	}
	page := 1
	listing := len(fields) == 1
	if len(fields) == 3 && fields[1] == "page" {
		n, e := strconv.Atoi(fields[2])
		if e == nil && n > 0 && n <= 100000 {
			page = n
			listing = true
		}
	}
	var items []privatechat.HistoricalInput
	var err error
	if listing {
		items, err = r.host.applications.journal.HistoricalInputPage(context.Background(), owner, generation, page, 25)
	}
	if len(fields) == 2 {
		if n, e := strconv.Atoi(fields[1]); e == nil && n > 0 {
			var item privatechat.HistoricalInput
			var found bool
			item, found, err = r.host.applications.journal.HistoricalInputAt(context.Background(), owner, generation, n)
			if found {
				items = []privatechat.HistoricalInput{item}
			}
		}
	}
	if err != nil {
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: &control.Notice{Code: "private_history_unavailable", Title: "历史输入", Text: "原始记录暂时无法读取，系统保留记录且不会自动重发。"}}}, true, nil
	}
	text := "没有可查看的历史输入。"
	var downloads []eventcontract.Event
	if listing && len(items) > 0 {
		var lines []string
		for i, item := range items {
			preview := []rune(strings.TrimSpace(item.Text))
			if len(preview) > 80 {
				preview = preview[:80]
			}
			lines = append(lines, fmt.Sprintf("%d. %s（%d 个附件）", (page-1)*25+i+1, string(preview), len(item.Attachments)))
		}
		text = "这些历史输入不会自动重发。发送 /recover 序号 查看原文，发送 /new 准备新会话，再发送 /recover confirm 序号 确认恢复。\n" + strings.Join(lines, "\n")
		if len(items) == 25 {
			text += fmt.Sprintf("\n发送 /recover page %d 查看下一页。", page+1)
		}
	}
	if len(fields) == 2 && fields[1] == "already" {
		text = "这条历史输入已经确认恢复，不会重复加入。"
	} else if len(fields) == 2 && fields[1] == "new-session" {
		text = "请先发送 /new 准备新会话，再发送 /recover confirm 序号。"
	} else if len(fields) == 2 && fields[1] == "unavailable" {
		text = "这条输入的附件暂时无法取回，尚未加入会话。"
	} else if len(fields) > 1 && !listing {
		n, e := strconv.Atoi(fields[1])
		if len(fields) != 2 || e != nil || n < 1 || len(items) != 1 {
			text = "请发送 /recover 查看列表，再发送 /recover 序号 查看原文，发送 /new 准备新会话，再发送 /recover confirm 序号 确认恢复。"
		} else {
			item := items[0]
			text = item.Text
			if !item.TextAvailable {
				text = "这条历史输入的原始文本暂时无法读取。"
			}
			for _, a := range item.Attachments {
				text += fmt.Sprintf("\n附件：%s", a.Attachment.FileName)
				if !a.Available {
					text += "（暂无法取回）"
					continue
				}
				root, e := os.OpenRoot(filepath.Dir(a.Path))
				if e != nil {
					text += "（暂无法验证）"
					continue
				}
				e = verifyPersonalAsset(root, filepath.Base(a.Path), a.Attachment.SHA256, a.Attachment.Size)
				root.Close()
				if e != nil {
					text += "（暂无法验证）"
					continue
				}
				downloads = append(downloads, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, DaemonCommand: &control.DaemonCommand{Kind: control.DaemonCommandPrivateSendFile, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, LocalPath: a.Path}})
			}
			if text == "" {
				text = "这条历史输入没有文本。"
			}
		}
	}
	events := []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: action.GatewayID, SurfaceSessionID: action.SurfaceSessionID, Notice: &control.Notice{Code: "private_history_recovery", Title: "历史输入", Text: text}}}
	return append(events, downloads...), true, nil
}
