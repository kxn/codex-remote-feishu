package feishu

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestMultiGatewayControllerRoutesApplyByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()

	app1 := runtimes.wait(t, "app-1")
	app2 := runtimes.wait(t, "app-2")
	waitFakeGatewayStarted(t, app1)
	waitFakeGatewayStarted(t, app2)

	err := controller.Apply(context.Background(), []Operation{
		{GatewayID: "app-1", Kind: OperationSendText, Text: "one"},
		{GatewayID: "app-2", Kind: OperationSendText, Text: "two"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	app1ApplyCalls := app1.applyCallsSnapshot()
	if len(app1ApplyCalls) != 1 || app1ApplyCalls[0][0].Text != "one" {
		t.Fatalf("unexpected app-1 apply calls: %#v", app1ApplyCalls)
	}
	app2ApplyCalls := app2.applyCallsSnapshot()
	if len(app2ApplyCalls) != 1 || app2ApplyCalls[0][0].Text != "two" {
		t.Fatalf("unexpected app-2 apply calls: %#v", app2ApplyCalls)
	}

	statuses := controller.Status()
	if len(statuses) != 2 {
		t.Fatalf("expected two statuses, got %#v", statuses)
	}
	for _, status := range statuses {
		if status.State != GatewayStateConnected {
			t.Fatalf("unexpected status: %#v", status)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for controller stop")
	}
}

func TestMultiGatewayControllerPropagatesMutatedOperationsBackToCaller(t *testing.T) {
	controller := NewMultiGatewayController()
	runtime := newFakeGatewayRuntime("app-1")
	runtime.applyFn = func(_ context.Context, operations []Operation) error {
		operations[0].MessageID = "om-progress-1"
		return nil
	}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := controller.UpsertApp(ctx, GatewayAppConfig{
		GatewayID: "app-1",
		AppID:     "cli_app-1",
		AppSecret: "secret_app-1",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	waitFakeGatewayStarted(t, runtime)

	operations := []Operation{{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		Kind:             OperationSendCard,
		CardTitle:        "执行命令",
		CardBody:         "处理中",
		CardThemeKey:     cardThemeInfo,
	}}
	if err := controller.Apply(context.Background(), operations); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if operations[0].MessageID != "om-progress-1" {
		t.Fatalf("expected mutated operation to propagate back to caller, got %#v", operations)
	}
}

func TestMultiGatewayControllerRoutesPreviewByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	previewers := newFakePreviewerRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}
	controller.newPreviewer = func(_ gatewayRuntime, cfg GatewayAppConfig) gatewayPreviewRuntime {
		previewer := &fakePreviewer{gatewayID: cfg.GatewayID}
		previewers.set(cfg.GatewayID, previewer)
		return previewer
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	waitFakeGatewayStarted(t, runtimes.wait(t, "app-1"))
	waitFakeGatewayStarted(t, runtimes.wait(t, "app-2"))

	result, err := controller.RewriteFinalBlock(context.Background(), previewpkg.FinalBlockPreviewRequest{
		GatewayID: "app-2",
		Block: render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Final: true,
			Text:  "hello",
		},
	})
	if err != nil {
		t.Fatalf("RewriteFinalBlock: %v", err)
	}
	if result.Block.Text != "app-2:hello" {
		t.Fatalf("unexpected rewritten block: %#v", result.Block)
	}
	app1Calls := previewers.get(t, "app-1").callsSnapshot()
	app2Calls := previewers.get(t, "app-2").callsSnapshot()
	if app1Calls != 0 || app2Calls != 1 {
		t.Fatalf("unexpected previewer calls: app-1=%d app-2=%d", app1Calls, app2Calls)
	}
}

func TestMultiGatewayControllerRoutesSendIMFileByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	app1 := runtimes.wait(t, "app-1")
	app2 := runtimes.wait(t, "app-2")
	waitFakeGatewayStarted(t, app1)
	waitFakeGatewayStarted(t, app2)

	result, err := controller.SendIMFile(context.Background(), IMFileSendRequest{
		GatewayID:        "app-2",
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/report.pdf",
	})
	if err != nil {
		t.Fatalf("SendIMFile: %v", err)
	}
	app1Calls := app1.sendIMFileCallsSnapshot()
	if len(app1Calls) != 0 {
		t.Fatalf("unexpected app-1 send calls: %#v", app1Calls)
	}
	app2Calls := app2.sendIMFileCallsSnapshot()
	if len(app2Calls) != 1 {
		t.Fatalf("unexpected app-2 send calls: %#v", app2Calls)
	}
	got := app2Calls[0]
	if got.SurfaceSessionID != "surface-2" || got.ChatID != "oc_2" || got.Path != "/tmp/report.pdf" {
		t.Fatalf("unexpected send request: %#v", got)
	}
	if result.GatewayID != "app-2" || result.MessageID != "msg-app-2" || result.FileName != "report.pdf" {
		t.Fatalf("unexpected send result: %#v", result)
	}
}

func TestMultiGatewayControllerRoutesSendIMImageByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	app1 := runtimes.wait(t, "app-1")
	app2 := runtimes.wait(t, "app-2")
	waitFakeGatewayStarted(t, app1)
	waitFakeGatewayStarted(t, app2)

	result, err := controller.SendIMImage(context.Background(), IMImageSendRequest{
		GatewayID:        "app-2",
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/preview.png",
	})
	if err != nil {
		t.Fatalf("SendIMImage: %v", err)
	}
	app1Calls := app1.sendIMImageCallsSnapshot()
	if len(app1Calls) != 0 {
		t.Fatalf("unexpected app-1 send calls: %#v", app1Calls)
	}
	app2Calls := app2.sendIMImageCallsSnapshot()
	if len(app2Calls) != 1 {
		t.Fatalf("unexpected app-2 send calls: %#v", app2Calls)
	}
	got := app2Calls[0]
	if got.SurfaceSessionID != "surface-2" || got.ChatID != "oc_2" || got.Path != "/tmp/preview.png" {
		t.Fatalf("unexpected send request: %#v", got)
	}
	if result.GatewayID != "app-2" || result.MessageID != "msg-app-2" || result.ImageName != "preview.png" {
		t.Fatalf("unexpected send result: %#v", result)
	}
}

func TestMultiGatewayControllerRoutesDriveCommentReadByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	app1 := runtimes.wait(t, "app-1")
	app2 := runtimes.wait(t, "app-2")
	waitFakeGatewayStarted(t, app1)
	waitFakeGatewayStarted(t, app2)

	result, err := controller.ReadDriveFileComments(context.Background(), DriveFileCommentReadRequest{
		GatewayID: "app-2",
		FileToken: "file-token-1",
		FileType:  "file",
	})
	if err != nil {
		t.Fatalf("ReadDriveFileComments: %v", err)
	}
	app1Calls := app1.readCommentCallsSnapshot()
	if len(app1Calls) != 0 {
		t.Fatalf("unexpected app-1 comment read calls: %#v", app1Calls)
	}
	app2Calls := app2.readCommentCallsSnapshot()
	if len(app2Calls) != 1 {
		t.Fatalf("unexpected app-2 comment read calls: %#v", app2Calls)
	}
	got := app2Calls[0]
	if got.FileToken != "file-token-1" || got.FileType != "file" {
		t.Fatalf("unexpected read request: %#v", got)
	}
	if result.GatewayID != "app-2" || result.CommentCount != 1 || len(result.Comments) != 1 {
		t.Fatalf("unexpected read result: %#v", result)
	}
}

func TestMultiGatewayControllerRoutesChatInfoReadByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := newFakeGatewayRuntimeRegistry()
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes.set(cfg.GatewayID, runtime)
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, gatewayID := range []string{"app-1", "app-2"} {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	app1 := runtimes.wait(t, "app-1")
	app2 := runtimes.wait(t, "app-2")
	waitFakeGatewayStarted(t, app1)
	waitFakeGatewayStarted(t, app2)

	info, err := controller.ReadChatInfo(context.Background(), ChatInfoRequest{
		GatewayID:        "app-2",
		SurfaceSessionID: "feishu:app-2:chat:oc_2",
		ChatID:           "oc_2",
	})
	if err != nil {
		t.Fatalf("ReadChatInfo: %v", err)
	}
	app1Calls := app1.readChatInfoCallsSnapshot()
	if len(app1Calls) != 0 {
		t.Fatalf("unexpected app-1 chat info calls: %#v", app1Calls)
	}
	app2Calls := app2.readChatInfoCallsSnapshot()
	if len(app2Calls) != 1 {
		t.Fatalf("unexpected app-2 chat info calls: %#v", app2Calls)
	}
	got := app2Calls[0]
	if got.GatewayID != "app-2" || got.SurfaceSessionID != "feishu:app-2:chat:oc_2" || got.ChatID != "oc_2" {
		t.Fatalf("unexpected chat info request: %#v", got)
	}
	if info.BotCount != 1 || info.ChatMode != "group" {
		t.Fatalf("unexpected chat info: %#v", info)
	}
}

func TestMultiGatewayControllerUpsertRestartsWorker(t *testing.T) {
	controller := NewMultiGatewayController()
	var (
		mu      sync.Mutex
		created []*fakeGatewayRuntime
	)
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		mu.Lock()
		created = append(created, runtime)
		mu.Unlock()
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := controller.UpsertApp(ctx, GatewayAppConfig{
		GatewayID: "app-1",
		AppID:     "cli_old",
		AppSecret: "secret_old",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("initial UpsertApp: %v", err)
	}

	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()

	first := waitForCreatedRuntime(t, &mu, &created, 0)
	waitFakeGatewayStarted(t, first)

	if err := controller.UpsertApp(ctx, GatewayAppConfig{
		GatewayID: "app-1",
		AppID:     "cli_new",
		AppSecret: "secret_new",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("second UpsertApp: %v", err)
	}

	second := waitForCreatedRuntime(t, &mu, &created, 1)
	waitFakeGatewayStarted(t, second)
	waitFakeGatewayStopped(t, first)
}

func TestMultiGatewayControllerRemoveDrainsActiveGenerationActions(t *testing.T) {
	controller := NewMultiGatewayController()
	runtime := newFakeGatewayRuntime("app-1")
	controller.newGateway = func(GatewayAppConfig) gatewayRuntime {
		return runtime
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := controller.UpsertApp(ctx, GatewayAppConfig{
		GatewayID: "app-1",
		AppID:     "cli_app-1",
		AppSecret: "secret_app-1",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var (
		handledMu sync.Mutex
		handled   int
	)
	go func() {
		_ = controller.Start(ctx, func(context.Context, control.Action) *ActionResult {
			handledMu.Lock()
			handled++
			current := handled
			handledMu.Unlock()
			if current == 1 {
				close(entered)
				<-release
			}
			return nil
		})
	}()
	waitFakeGatewayStarted(t, runtime)

	firstDone := make(chan struct{})
	go func() {
		runtime.dispatch(context.Background(), control.Action{GatewayID: "app-1"})
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active generation action")
	}

	removeDone := make(chan error, 1)
	go func() {
		removeDone <- controller.RemoveApp(context.Background(), "app-1")
	}()
	waitFakeGatewayStopped(t, runtime)
	select {
	case err := <-removeDone:
		t.Fatalf("RemoveApp returned before active action drained: %v", err)
	default:
	}

	runtime.dispatch(context.Background(), control.Action{GatewayID: "app-1"})
	close(release)
	select {
	case <-firstDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active action to finish")
	}
	select {
	case err := <-removeDone:
		if err != nil {
			t.Fatalf("RemoveApp: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for RemoveApp")
	}

	handledMu.Lock()
	defer handledMu.Unlock()
	if handled != 1 {
		t.Fatalf("handled actions = %d, want only the action admitted before removal", handled)
	}
}

func TestMultiGatewayControllerStartsAndStopsPreviewMaintenance(t *testing.T) {
	controller := NewMultiGatewayController()
	runtime := newFakeGatewayRuntime("app-1")
	previewer := &fakePreviewer{
		gatewayID:          "app-1",
		maintenanceStarted: make(chan struct{}, 1),
		maintenanceStopped: make(chan struct{}, 1),
	}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		return runtime
	}
	controller.newPreviewer = func(_ gatewayRuntime, cfg GatewayAppConfig) gatewayPreviewRuntime {
		previewer.gatewayID = cfg.GatewayID
		return previewer
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := controller.UpsertApp(ctx, GatewayAppConfig{
		GatewayID: "app-1",
		AppID:     "cli_app-1",
		AppSecret: "secret_app-1",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()

	waitFakeGatewayStarted(t, runtime)
	select {
	case <-previewer.maintenanceStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for preview maintenance to start")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for controller stop")
	}
	select {
	case <-previewer.maintenanceStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for preview maintenance to stop")
	}
}

func TestMultiGatewayControllerStatusShowsDisabledApps(t *testing.T) {
	controller := NewMultiGatewayController()
	if err := controller.UpsertApp(context.Background(), GatewayAppConfig{
		GatewayID: "app-1",
		Name:      "App 1",
		Enabled:   false,
	}); err != nil {
		t.Fatalf("UpsertApp: %v", err)
	}
	statuses := controller.Status()
	if len(statuses) != 1 || !statuses[0].Disabled || statuses[0].State != GatewayStateDisabled {
		t.Fatalf("unexpected disabled status: %#v", statuses)
	}
}

type fakeGatewayRuntime struct {
	gatewayID string
	startedCh chan struct{}
	stoppedCh chan struct{}

	mu                sync.Mutex
	stateHook         func(GatewayState, error)
	actionHandler     ActionHandler
	applyCalls        [][]Operation
	applyFn           func(context.Context, []Operation) error
	sendIMFileCalls   []IMFileSendRequest
	sendIMFileFn      func(context.Context, IMFileSendRequest) (IMFileSendResult, error)
	sendIMImageCalls  []IMImageSendRequest
	sendIMImageFn     func(context.Context, IMImageSendRequest) (IMImageSendResult, error)
	sendIMVideoCalls  []IMVideoSendRequest
	sendIMVideoFn     func(context.Context, IMVideoSendRequest) (IMVideoSendResult, error)
	readChatInfoCalls []ChatInfoRequest
	readChatInfoFn    func(context.Context, ChatInfoRequest) (ChatInfo, error)
	readCommentCalls  []DriveFileCommentReadRequest
	readCommentFn     func(context.Context, DriveFileCommentReadRequest) (DriveFileCommentReadResult, error)
}

type fakeGatewayRuntimeRegistry struct {
	mu       sync.Mutex
	runtimes map[string]*fakeGatewayRuntime
	waiters  map[string][]chan struct{}
}

func newFakeGatewayRuntimeRegistry() *fakeGatewayRuntimeRegistry {
	return &fakeGatewayRuntimeRegistry{
		runtimes: map[string]*fakeGatewayRuntime{},
		waiters:  map[string][]chan struct{}{},
	}
}

func (r *fakeGatewayRuntimeRegistry) set(gatewayID string, runtime *fakeGatewayRuntime) {
	r.mu.Lock()
	r.runtimes[gatewayID] = runtime
	waiters := r.waiters[gatewayID]
	delete(r.waiters, gatewayID)
	r.mu.Unlock()

	for _, waiter := range waiters {
		close(waiter)
	}
}

func (r *fakeGatewayRuntimeRegistry) get(t *testing.T, gatewayID string) *fakeGatewayRuntime {
	t.Helper()

	r.mu.Lock()
	runtime := r.runtimes[gatewayID]
	r.mu.Unlock()
	if runtime == nil {
		t.Fatalf("runtime %s was not created", gatewayID)
	}
	return runtime
}

func (r *fakeGatewayRuntimeRegistry) wait(t *testing.T, gatewayID string) *fakeGatewayRuntime {
	t.Helper()

	for {
		r.mu.Lock()
		if runtime := r.runtimes[gatewayID]; runtime != nil {
			r.mu.Unlock()
			return runtime
		}
		waiter := make(chan struct{})
		r.waiters[gatewayID] = append(r.waiters[gatewayID], waiter)
		r.mu.Unlock()

		select {
		case <-waiter:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for runtime %s to be created", gatewayID)
		}
	}
}

func newFakeGatewayRuntime(gatewayID string) *fakeGatewayRuntime {
	return &fakeGatewayRuntime{
		gatewayID: gatewayID,
		startedCh: make(chan struct{}, 1),
		stoppedCh: make(chan struct{}, 1),
	}
}

func (f *fakeGatewayRuntime) Start(ctx context.Context, handler ActionHandler) error {
	f.mu.Lock()
	f.actionHandler = handler
	f.mu.Unlock()
	f.emitState(GatewayStateConnected, nil)
	select {
	case f.startedCh <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case f.stoppedCh <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeGatewayRuntime) dispatch(ctx context.Context, action control.Action) *ActionResult {
	f.mu.Lock()
	handler := f.actionHandler
	f.mu.Unlock()
	if handler == nil {
		return nil
	}
	return handler(ctx, action)
}

func (f *fakeGatewayRuntime) Apply(_ context.Context, operations []Operation) error {
	f.mu.Lock()
	fn := f.applyFn
	f.applyCalls = append(f.applyCalls, append([]Operation(nil), operations...))
	f.mu.Unlock()
	if fn != nil {
		return fn(context.Background(), operations)
	}
	return nil
}

func (f *fakeGatewayRuntime) applyCallsSnapshot() [][]Operation {
	f.mu.Lock()
	defer f.mu.Unlock()

	calls := make([][]Operation, 0, len(f.applyCalls))
	for _, call := range f.applyCalls {
		calls = append(calls, append([]Operation(nil), call...))
	}
	return calls
}

func (f *fakeGatewayRuntime) SendIMFile(ctx context.Context, req IMFileSendRequest) (IMFileSendResult, error) {
	f.mu.Lock()
	f.sendIMFileCalls = append(f.sendIMFileCalls, req)
	fn := f.sendIMFileFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return IMFileSendResult{
		GatewayID:        f.gatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		FileName:         filepath.Base(req.Path),
		FileKey:          "file-key-" + f.gatewayID,
		MessageID:        "msg-" + f.gatewayID,
	}, nil
}

func (f *fakeGatewayRuntime) sendIMFileCallsSnapshot() []IMFileSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]IMFileSendRequest(nil), f.sendIMFileCalls...)
}

func (f *fakeGatewayRuntime) SendIMImage(ctx context.Context, req IMImageSendRequest) (IMImageSendResult, error) {
	f.mu.Lock()
	f.sendIMImageCalls = append(f.sendIMImageCalls, req)
	fn := f.sendIMImageFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return IMImageSendResult{
		GatewayID:        f.gatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		ImageName:        filepath.Base(req.Path),
		ImageKey:         "image-key-" + f.gatewayID,
		MessageID:        "msg-" + f.gatewayID,
	}, nil
}

func (f *fakeGatewayRuntime) sendIMImageCallsSnapshot() []IMImageSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]IMImageSendRequest(nil), f.sendIMImageCalls...)
}

func (f *fakeGatewayRuntime) SendIMVideo(ctx context.Context, req IMVideoSendRequest) (IMVideoSendResult, error) {
	f.mu.Lock()
	f.sendIMVideoCalls = append(f.sendIMVideoCalls, req)
	fn := f.sendIMVideoFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return IMVideoSendResult{
		GatewayID:        f.gatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		VideoName:        filepath.Base(req.Path),
		FileKey:          "video-key-" + f.gatewayID,
		MessageID:        "msg-" + f.gatewayID,
	}, nil
}

func (f *fakeGatewayRuntime) sendIMVideoCallsSnapshot() []IMVideoSendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]IMVideoSendRequest(nil), f.sendIMVideoCalls...)
}

func (f *fakeGatewayRuntime) ReadChatInfo(ctx context.Context, req ChatInfoRequest) (ChatInfo, error) {
	f.mu.Lock()
	f.readChatInfoCalls = append(f.readChatInfoCalls, req)
	fn := f.readChatInfoFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return ChatInfo{
		BotCount: 1,
		ChatMode: "group",
	}, nil
}

func (f *fakeGatewayRuntime) readChatInfoCallsSnapshot() []ChatInfoRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]ChatInfoRequest(nil), f.readChatInfoCalls...)
}

func (f *fakeGatewayRuntime) ReadDriveFileComments(ctx context.Context, req DriveFileCommentReadRequest) (DriveFileCommentReadResult, error) {
	f.mu.Lock()
	f.readCommentCalls = append(f.readCommentCalls, req)
	fn := f.readCommentFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return DriveFileCommentReadResult{
		GatewayID:        f.gatewayID,
		FileToken:        req.FileToken,
		FileType:         req.FileType,
		StatsScope:       "returned_comments_page",
		CommentCount:     1,
		ReplyCount:       0,
		InteractionCount: 1,
		Comments: []DriveFileCommentEntry{
			{
				CommentID: "comment-" + f.gatewayID,
				UserID:    "ou_" + f.gatewayID,
				Replies: []DriveFileCommentReplyItem{
					{ReplyID: "reply-" + f.gatewayID, UserID: "ou_" + f.gatewayID, Text: "looks good"},
				},
			},
		},
	}, nil
}

func (f *fakeGatewayRuntime) readCommentCallsSnapshot() []DriveFileCommentReadRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]DriveFileCommentReadRequest(nil), f.readCommentCalls...)
}

func (f *fakeGatewayRuntime) Client() *lark.Client { return nil }

func (f *fakeGatewayRuntime) SetStateHook(hook func(GatewayState, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateHook = hook
}

func (f *fakeGatewayRuntime) emitState(state GatewayState, err error) {
	f.mu.Lock()
	hook := f.stateHook
	f.mu.Unlock()
	if hook != nil {
		hook(state, err)
	}
}

type fakePreviewer struct {
	mu                 sync.Mutex
	gatewayID          string
	calls              int
	maintenanceStarted chan struct{}
	maintenanceStopped chan struct{}
}

type fakePreviewerRegistry struct {
	mu         sync.Mutex
	previewers map[string]*fakePreviewer
}

func newFakePreviewerRegistry() *fakePreviewerRegistry {
	return &fakePreviewerRegistry{previewers: map[string]*fakePreviewer{}}
}

func (r *fakePreviewerRegistry) set(gatewayID string, previewer *fakePreviewer) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.previewers[gatewayID] = previewer
}

func (r *fakePreviewerRegistry) get(t *testing.T, gatewayID string) *fakePreviewer {
	t.Helper()

	r.mu.Lock()
	previewer := r.previewers[gatewayID]
	r.mu.Unlock()
	if previewer == nil {
		t.Fatalf("previewer %s was not created", gatewayID)
	}
	return previewer
}

func (f *fakePreviewer) RewriteFinalBlock(_ context.Context, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	f.mu.Lock()
	f.calls++
	gatewayID := f.gatewayID
	f.mu.Unlock()

	block := req.Block
	block.Text = gatewayID + ":" + block.Text
	return previewpkg.FinalBlockPreviewResult{Block: block}, nil
}

func (f *fakePreviewer) callsSnapshot() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *fakePreviewer) SetWebPreviewPublisher(previewpkg.WebPreviewPublisher) {}

func (f *fakePreviewer) ServeWebPreview(http.ResponseWriter, *http.Request, string, string, bool) bool {
	return false
}

func (f *fakePreviewer) RunBackgroundMaintenance(ctx context.Context) {
	if f.maintenanceStarted != nil {
		select {
		case f.maintenanceStarted <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	if f.maintenanceStopped != nil {
		select {
		case f.maintenanceStopped <- struct{}{}:
		default:
		}
	}
}

func waitFakeGatewayStarted(t *testing.T, runtime *fakeGatewayRuntime) {
	t.Helper()
	select {
	case <-runtime.startedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for gateway %s to start", runtime.gatewayID)
	}
}

func waitFakeGatewayStopped(t *testing.T, runtime *fakeGatewayRuntime) {
	t.Helper()
	select {
	case <-runtime.stoppedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for gateway %s to stop", runtime.gatewayID)
	}
}

func waitForCreatedRuntime(t *testing.T, mu *sync.Mutex, created *[]*fakeGatewayRuntime, index int) *fakeGatewayRuntime {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		mu.Lock()
		if len(*created) > index {
			runtime := (*created)[index]
			mu.Unlock()
			return runtime
		}
		mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for created runtime index %d", index)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
