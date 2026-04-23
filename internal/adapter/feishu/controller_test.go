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
	runtimes := map[string]*fakeGatewayRuntime{}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes[cfg.GatewayID] = runtime
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

	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-1"))
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-2"))

	err := controller.Apply(context.Background(), []Operation{
		{GatewayID: "app-1", Kind: OperationSendText, Text: "one"},
		{GatewayID: "app-2", Kind: OperationSendText, Text: "two"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(runtimes["app-1"].applyCalls) != 1 || runtimes["app-1"].applyCalls[0][0].Text != "one" {
		t.Fatalf("unexpected app-1 apply calls: %#v", runtimes["app-1"].applyCalls)
	}
	if len(runtimes["app-2"].applyCalls) != 1 || runtimes["app-2"].applyCalls[0][0].Text != "two" {
		t.Fatalf("unexpected app-2 apply calls: %#v", runtimes["app-2"].applyCalls)
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
	runtimes := map[string]*fakeGatewayRuntime{}
	previewers := map[string]*fakePreviewer{}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes[cfg.GatewayID] = runtime
		return runtime
	}
	controller.newPreviewer = func(_ gatewayRuntime, cfg GatewayAppConfig) gatewayPreviewRuntime {
		previewer := &fakePreviewer{gatewayID: cfg.GatewayID}
		previewers[cfg.GatewayID] = previewer
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
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-1"))
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-2"))

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
	if previewers["app-1"].calls != 0 || previewers["app-2"].calls != 1 {
		t.Fatalf("unexpected previewer calls: app-1=%d app-2=%d", previewers["app-1"].calls, previewers["app-2"].calls)
	}
}

func TestMultiGatewayControllerRoutesSendIMFileByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := map[string]*fakeGatewayRuntime{}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes[cfg.GatewayID] = runtime
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
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-1"))
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-2"))

	result, err := controller.SendIMFile(context.Background(), IMFileSendRequest{
		GatewayID:        "app-2",
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/report.pdf",
	})
	if err != nil {
		t.Fatalf("SendIMFile: %v", err)
	}
	if len(runtimes["app-1"].sendIMFileCalls) != 0 {
		t.Fatalf("unexpected app-1 send calls: %#v", runtimes["app-1"].sendIMFileCalls)
	}
	if len(runtimes["app-2"].sendIMFileCalls) != 1 {
		t.Fatalf("unexpected app-2 send calls: %#v", runtimes["app-2"].sendIMFileCalls)
	}
	got := runtimes["app-2"].sendIMFileCalls[0]
	if got.SurfaceSessionID != "surface-2" || got.ChatID != "oc_2" || got.Path != "/tmp/report.pdf" {
		t.Fatalf("unexpected send request: %#v", got)
	}
	if result.GatewayID != "app-2" || result.MessageID != "msg-app-2" || result.FileName != "report.pdf" {
		t.Fatalf("unexpected send result: %#v", result)
	}
}

func TestMultiGatewayControllerRoutesSendIMImageByGatewayID(t *testing.T) {
	controller := NewMultiGatewayController()
	runtimes := map[string]*fakeGatewayRuntime{}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes[cfg.GatewayID] = runtime
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
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-1"))
	waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, "app-2"))

	result, err := controller.SendIMImage(context.Background(), IMImageSendRequest{
		GatewayID:        "app-2",
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/preview.png",
	})
	if err != nil {
		t.Fatalf("SendIMImage: %v", err)
	}
	if len(runtimes["app-1"].sendIMImageCalls) != 0 {
		t.Fatalf("unexpected app-1 send calls: %#v", runtimes["app-1"].sendIMImageCalls)
	}
	if len(runtimes["app-2"].sendIMImageCalls) != 1 {
		t.Fatalf("unexpected app-2 send calls: %#v", runtimes["app-2"].sendIMImageCalls)
	}
	got := runtimes["app-2"].sendIMImageCalls[0]
	if got.SurfaceSessionID != "surface-2" || got.ChatID != "oc_2" || got.Path != "/tmp/preview.png" {
		t.Fatalf("unexpected send request: %#v", got)
	}
	if result.GatewayID != "app-2" || result.MessageID != "msg-app-2" || result.ImageName != "preview.png" {
		t.Fatalf("unexpected send result: %#v", result)
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

	mu               sync.Mutex
	stateHook        func(GatewayState, error)
	applyCalls       [][]Operation
	applyFn          func(context.Context, []Operation) error
	sendIMFileCalls  []IMFileSendRequest
	sendIMFileFn     func(context.Context, IMFileSendRequest) (IMFileSendResult, error)
	sendIMImageCalls []IMImageSendRequest
	sendIMImageFn    func(context.Context, IMImageSendRequest) (IMImageSendResult, error)
}

func newFakeGatewayRuntime(gatewayID string) *fakeGatewayRuntime {
	return &fakeGatewayRuntime{
		gatewayID: gatewayID,
		startedCh: make(chan struct{}, 1),
		stoppedCh: make(chan struct{}, 1),
	}
}

func (f *fakeGatewayRuntime) Start(ctx context.Context, _ ActionHandler) error {
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
	gatewayID          string
	calls              int
	maintenanceStarted chan struct{}
	maintenanceStopped chan struct{}
}

func (f *fakePreviewer) RewriteFinalBlock(_ context.Context, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	f.calls++
	block := req.Block
	block.Text = f.gatewayID + ":" + block.Text
	return previewpkg.FinalBlockPreviewResult{Block: block}, nil
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

func waitForFakeRuntime(t *testing.T, runtimes map[string]*fakeGatewayRuntime, gatewayID string) *fakeGatewayRuntime {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if runtime := runtimes[gatewayID]; runtime != nil {
			return runtime
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for runtime %s to be created", gatewayID)
		default:
			time.Sleep(10 * time.Millisecond)
		}
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
