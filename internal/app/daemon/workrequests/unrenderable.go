package workrequests

import (
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
)

func queueUnrenderable(tx *workstore.Tx, request workstore.NativeRequest, _ error) error {
	view, err := workui.Destination(tx, request.ScopeID, request.Binding.RoleID, request.Binding.AttemptID)
	if err != nil {
		return err
	}
	view.ID = workui.ID(request.ID, "unrenderable", strconv.FormatInt(request.Revision, 10))
	view.SurfaceKey = "request:" + request.ID
	view.Kind = "native.request"
	view.TargetID = request.ID
	view.TargetRevision = request.Revision
	view.ContextID = request.Binding.ContextID
	card, err := workui.Card(view.ID, workui.Panel{Title: "这项原生请求需要另行处理", Paragraphs: []string{"当前请求不能在飞书中完整显示，原始内容已经保存，不会自动批准。可以先暂停这项工作，再处理原生请求。"}})
	if err != nil {
		return err
	}
	_, err = workui.Queue(tx, view, card)
	return err
}
