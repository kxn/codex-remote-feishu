package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type messageIDAssigningFileGateway struct {
	*messageIDAssigningGateway
	sendFn func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error)
	calls  []feishu.IMFileSendRequest
}

func newMessageIDAssigningFileGateway() *messageIDAssigningFileGateway {
	return &messageIDAssigningFileGateway{
		messageIDAssigningGateway: &messageIDAssigningGateway{notify: make(chan struct{}, 16)},
	}
}

func (g *messageIDAssigningFileGateway) SendIMFile(ctx context.Context, req feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
	g.mu.Lock()
	g.calls = append(g.calls, req)
	g.mu.Unlock()
	if g.sendFn != nil {
		return g.sendFn(ctx, req)
	}
	return feishu.IMFileSendResult{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		FileName:         filepath.Base(req.Path),
		FileKey:          "file-key",
		MessageID:        "msg-file",
	}, nil
}

func (g *messageIDAssigningFileGateway) snapshotSendCalls() []feishu.IMFileSendRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]feishu.IMFileSendRequest(nil), g.calls...)
}

func TestHandleSendIMFileCommandSendsFileToCurrentSurface(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	events := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filePath,
	})

	if len(sender.calls) != 1 {
		t.Fatalf("expected one send call, got %#v", sender.calls)
	}
	if sender.calls[0].SurfaceSessionID != "surface-1" || sender.calls[0].ChatID != "chat-1" || sender.calls[0].ActorUserID != "user-1" {
		t.Fatalf("unexpected send call: %#v", sender.calls[0])
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_sent" {
		t.Fatalf("expected send success notice, got %#v", events)
	}
}

func TestHandleSendIMFileCommandRejectsMissingFileAndDetachedSurface(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})

	missing := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filepath.Join(t.TempDir(), "missing.txt"),
	})
	if len(missing) != 1 || missing[0].Notice == nil || missing[0].Notice.Code != "send_file_not_found" {
		t.Fatalf("expected missing-file notice, got %#v", missing)
	}

	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	detached := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-unknown",
		LocalPath:        filePath,
	})
	if len(detached) != 1 || detached[0].Notice == nil || detached[0].Notice.Code != "send_file_unavailable" {
		t.Fatalf("expected detached-surface notice, got %#v", detached)
	}
}

func TestHandleSendIMFileCommandMapsUploadAndSendFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{
			name:     "upload failed",
			err:      &feishu.IMFileSendError{Code: feishu.IMFileSendErrorUploadFailed, Err: errors.New("upload failed")},
			wantCode: "send_file_upload_failed",
		},
		{
			name:     "send failed",
			err:      &feishu.IMFileSendError{Code: feishu.IMFileSendErrorSendFailed, Err: errors.New("send failed")},
			wantCode: "send_file_failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeToolFileSender{
				sendFn: func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
					return feishu.IMFileSendResult{}, tc.err
				},
			}
			app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
			workspaceRoot := t.TempDir()
			filePath := filepath.Join(workspaceRoot, "report.txt")
			if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			app.service.UpsertInstance(&state.InstanceRecord{
				InstanceID:    "inst-1",
				WorkspaceRoot: workspaceRoot,
				WorkspaceKey:  workspaceRoot,
				Source:        "headless",
				Online:        true,
				Threads:       map[string]*state.ThreadRecord{},
			})
			app.HandleAction(context.Background(), control.Action{
				Kind:             control.ActionAttachInstance,
				SurfaceSessionID: "surface-1",
				GatewayID:        "app-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				InstanceID:       "inst-1",
			})

			events := app.handleSendIMFileCommand(control.DaemonCommand{
				Kind:             control.DaemonCommandSendIMFile,
				SurfaceSessionID: "surface-1",
				LocalPath:        filePath,
			})
			if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != tc.wantCode {
				t.Fatalf("expected %s notice, got %#v", tc.wantCode, events)
			}
		})
	}
}

func TestHandleSendIMFileCommandReleasesAppLockAndUsesDeadline(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	sender.sendFn = func(ctx context.Context, req feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
		assertDaemonContextHasDeadlineWithin(t, ctx, sendIMFileCommandTimeout)

		lockAcquired := make(chan struct{})
		go func() {
			app.mu.Lock()
			close(lockAcquired)
			app.mu.Unlock()
		}()
		select {
		case <-lockAcquired:
		case <-time.After(time.Second):
			t.Fatal("app mutex remained locked during SendIMFile")
		}

		return feishu.IMFileSendResult{
			GatewayID:        req.GatewayID,
			SurfaceSessionID: req.SurfaceSessionID,
			FileName:         filepath.Base(req.Path),
			FileKey:          "file-key",
			MessageID:        "msg-file",
		}, nil
	}

	events := app.handleSendIMFileCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandSendIMFile,
		SurfaceSessionID: "surface-1",
		LocalPath:        filePath,
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "send_file_sent" {
		t.Fatalf("expected send success notice, got %#v", events)
	}
}

func TestHandleActionPathPickerConfirmSendFileDoesNotDeadlock(t *testing.T) {
	sender := &fakeToolFileSender{}
	app := New(":0", ":0", sender, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	waitAction := func(name string, fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() {
			fn()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s timed out (possible deadlock)", name)
		}
	}

	waitAction("open send file picker", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionSendFile,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
		})
	})
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected active path picker, got %#v", surfaces)
	}
	runtime := app.service.SurfaceUIRuntime("surface-1")
	if runtime.ActivePathPickerID == "" {
		t.Fatalf("expected active path picker, got runtime=%#v surfaces=%#v", runtime, surfaces)
	}
	pickerID := runtime.ActivePathPickerID
	if pickerID == "" {
		t.Fatalf("expected picker id")
	}

	waitAction("select file", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionPathPickerSelect,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			PickerID:         pickerID,
			PickerEntry:      filepath.Base(filePath),
		})
	})
	waitAction("confirm picker and send file", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionPathPickerConfirm,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			PickerID:         pickerID,
		})
	})
	waitAction("follow-up status action", func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionStatus,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
		})
	})

	if len(sender.calls) != 1 {
		t.Fatalf("expected one send call after confirm, got %#v", sender.calls)
	}
}

func TestHandleActionPathPickerConfirmSendFileSealsCurrentCardAndSuppressesSuccessNotice(t *testing.T) {
	gateway := newMessageIDAssigningFileGateway()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "large.bin")
	if err := os.WriteFile(filePath, nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Truncate(filePath, 101*1024*1024); err != nil {
		t.Fatalf("Truncate() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	runtime := app.service.SurfaceUIRuntime("surface-1")
	pickerID := runtime.ActivePathPickerID
	if pickerID == "" {
		t.Fatalf("expected active path picker, got %#v", runtime)
	}
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
		PickerEntry:      filepath.Base(filePath),
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
	})

	ops := gateway.waitForOperationCount(4, 2*time.Second)
	if len(ops) < 4 {
		t.Fatalf("expected attach notice + picker send/select + terminal update, got %#v", ops)
	}
	var latestPickerCard *feishu.Operation
	var terminalUpdate *feishu.Operation
	for i := range ops {
		op := &ops[i]
		if op.Kind == feishu.OperationSendCard && op.CardTitle == "选择要发送的文件" {
			latestPickerCard = op
		}
		if op.Kind == feishu.OperationUpdateCard && strings.Contains(strings.Join(cardMarkdownContents(op.CardElements), "\n"), "已开始发送，可继续其他操作") {
			terminalUpdate = op
		}
	}
	if latestPickerCard == nil || terminalUpdate == nil {
		t.Fatalf("expected picker send card and terminal update, got %#v", ops)
	}
	if terminalUpdate.MessageID != latestPickerCard.MessageID {
		t.Fatalf("expected terminal update to target latest picker card, got card=%#v update=%#v", latestPickerCard, terminalUpdate)
	}
	if got := app.service.SurfaceUIRuntime("surface-1").ActivePathPickerID; got != "" {
		t.Fatalf("expected picker gate released after start, got %q", got)
	}
	cardText := strings.Join(cardMarkdownContents(terminalUpdate.CardElements), "\n")
	for _, want := range []string{
		"已开始发送，可继续其他操作",
		"large.bin",
		"101.0 MB",
		"文件较大，请耐心等待",
	} {
		if !strings.Contains(cardText, want) {
			t.Fatalf("expected terminal card to contain %q, got %q", want, cardText)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(gateway.snapshotSendCalls()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(gateway.snapshotSendCalls()) != 1 {
		t.Fatalf("expected one background send call, got %#v", gateway.snapshotSendCalls())
	}
	time.Sleep(100 * time.Millisecond)
	for _, op := range gateway.snapshotOperations() {
		cardText := op.CardBody + "\n" + strings.Join(cardMarkdownContents(op.CardElements), "\n")
		if strings.Contains(cardText, "已把 `large.bin` 发送到当前聊天") {
			t.Fatalf("expected no extra success notice card, got %#v", gateway.snapshotOperations())
		}
	}
}

func TestHandleActionPathPickerConfirmSendFilePreflightFailureKeepsPickerOnSameCard(t *testing.T) {
	gateway := &messageIDAssigningGateway{notify: make(chan struct{}, 16)}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	pickerID := app.service.SurfaceUIRuntime("surface-1").ActivePathPickerID
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
		PickerEntry:      filepath.Base(filePath),
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
	})

	ops := gateway.waitForOperationCount(4, 2*time.Second)
	if len(ops) < 4 {
		t.Fatalf("expected attach notice + picker send/select + preflight failure update, got %#v", ops)
	}
	var latestPickerCard *feishu.Operation
	var failureUpdate *feishu.Operation
	for i := range ops {
		op := &ops[i]
		if op.Kind == feishu.OperationSendCard && op.CardTitle == "选择要发送的文件" {
			latestPickerCard = op
		}
		if op.Kind == feishu.OperationUpdateCard && strings.Contains(strings.Join(cardMarkdownContents(op.CardElements), "\n"), "暂不支持发送飞书文件消息") {
			failureUpdate = op
		}
	}
	if latestPickerCard == nil || failureUpdate == nil {
		t.Fatalf("expected picker failure to patch same card, got %#v", ops)
	}
	if failureUpdate.MessageID != latestPickerCard.MessageID {
		t.Fatalf("expected preflight failure to update latest picker card, got card=%#v update=%#v", latestPickerCard, failureUpdate)
	}
	cardText := strings.Join(cardMarkdownContents(failureUpdate.CardElements), "\n")
	if !strings.Contains(cardText, "暂不支持发送飞书文件消息") {
		t.Fatalf("expected unsupported-sender hint on current card, got %q", cardText)
	}
	if got := app.service.SurfaceUIRuntime("surface-1").ActivePathPickerID; got != pickerID {
		t.Fatalf("expected picker to remain active after preflight failure, got %q want %q", got, pickerID)
	}
}

func TestHandleActionPathPickerConfirmSendFileBackgroundFailureEmitsLightNotice(t *testing.T) {
	gateway := newMessageIDAssigningFileGateway()
	gateway.sendFn = func(context.Context, feishu.IMFileSendRequest) (feishu.IMFileSendResult, error) {
		return feishu.IMFileSendResult{}, &feishu.IMFileSendError{
			Code: feishu.IMFileSendErrorUploadFailed,
			Err:  errors.New("upload failed"),
		}
	}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionSendFile,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	pickerID := app.service.SurfaceUIRuntime("surface-1").ActivePathPickerID
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerSelect,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
		PickerEntry:      filepath.Base(filePath),
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pickerID,
	})

	if got := app.service.SurfaceUIRuntime("surface-1").ActivePathPickerID; got != "" {
		t.Fatalf("expected picker gate released after background failure, got %q", got)
	}
	var failureCard string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range gateway.snapshotOperations() {
			cardText := op.CardBody + "\n" + strings.Join(cardMarkdownContents(op.CardElements), "\n")
			if strings.Contains(cardText, "文件上传失败，请稍后重试") {
				failureCard = cardText
				break
			}
		}
		if failureCard != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failureCard == "" {
		t.Fatalf("expected light failure notice after background send failure, got %#v", gateway.snapshotOperations())
	}
}

func cardMarkdownContents(elements []map[string]any) []string {
	var contents []string
	for _, element := range elements {
		switch element["tag"] {
		case "markdown":
			content, ok := element["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				continue
			}
			contents = append(contents, content)
		case "div":
			text, _ := element["text"].(map[string]any)
			if text["tag"] != "plain_text" {
				continue
			}
			content, ok := text["content"].(string)
			if !ok || strings.TrimSpace(content) == "" {
				continue
			}
			contents = append(contents, content)
		}
	}
	return contents
}

func assertDaemonContextHasDeadlineWithin(t *testing.T, ctx context.Context, max time.Duration) {
	t.Helper()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("expected future deadline, got %s", deadline)
	}
	if remaining > max+time.Second {
		t.Fatalf("expected deadline within %s, got remaining %s", max, remaining)
	}
}
