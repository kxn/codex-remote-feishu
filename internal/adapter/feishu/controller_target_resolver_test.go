package feishu

import (
	"context"
	"errors"
	"testing"
	"time"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type testGatewayWorkerState struct {
	runtime   bool
	previewer bool
}

func TestMultiGatewayControllerResolveGatewayTargetMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		workers       map[string]testGatewayWorkerState
		target        eventcontract.TargetRef
		requirement   gatewayTargetRequirement
		wantOutcome   gatewayTargetOutcome
		wantGatewayID string
	}{
		{
			name: "explicit runtime target resolves running gateway",
			workers: map[string]testGatewayWorkerState{
				"app-1": {runtime: true},
			},
			target:        eventcontract.ExplicitTarget("app-1", ""),
			requirement:   gatewayTargetRequireRuntime,
			wantOutcome:   gatewayTargetResolved,
			wantGatewayID: "app-1",
		},
		{
			name: "sole gateway fallback resolves single configured worker",
			workers: map[string]testGatewayWorkerState{
				"app-1": {runtime: true},
			},
			target: eventcontract.TargetRef{
				SelectionPolicy: eventcontract.GatewaySelectionAllowSoleGatewayFallback,
				FailurePolicy:   eventcontract.GatewayFailureError,
			},
			requirement:   gatewayTargetRequireRuntime,
			wantOutcome:   gatewayTargetResolved,
			wantGatewayID: "app-1",
		},
		{
			name: "sole gateway fallback fails when multiple gateways exist",
			workers: map[string]testGatewayWorkerState{
				"app-1": {runtime: true},
				"app-2": {runtime: true},
			},
			target: eventcontract.TargetRef{
				SelectionPolicy: eventcontract.GatewaySelectionAllowSoleGatewayFallback,
				FailurePolicy:   eventcontract.GatewayFailureError,
			},
			requirement: gatewayTargetRequireRuntime,
			wantOutcome: gatewayTargetMissingGatewayID,
		},
		{
			name: "surface fallback resolves preview gateway from surface session",
			workers: map[string]testGatewayWorkerState{
				"app-1": {previewer: true},
				"app-2": {previewer: true},
			},
			target: eventcontract.TargetRef{
				SurfaceSessionID: "feishu:app-2:user:user-1",
				SelectionPolicy:  eventcontract.GatewaySelectionAllowSurfaceDerived,
				FailurePolicy:    eventcontract.GatewayFailureNoop,
			},
			requirement:   gatewayTargetRequirePreviewer,
			wantOutcome:   gatewayTargetResolved,
			wantGatewayID: "app-2",
		},
		{
			name: "surface fallback noops when no target can be derived",
			workers: map[string]testGatewayWorkerState{
				"app-1": {previewer: true},
			},
			target: eventcontract.TargetRef{
				SelectionPolicy: eventcontract.GatewaySelectionAllowSurfaceDerived,
				FailurePolicy:   eventcontract.GatewayFailureNoop,
			},
			requirement: gatewayTargetRequirePreviewer,
			wantOutcome: gatewayTargetNoop,
		},
		{
			name: "preview requirement noops when gateway has no previewer",
			workers: map[string]testGatewayWorkerState{
				"app-1": {runtime: true},
			},
			target: eventcontract.TargetRef{
				GatewayID:       "app-1",
				SelectionPolicy: eventcontract.GatewaySelectionExplicitOnly,
				FailurePolicy:   eventcontract.GatewayFailureNoop,
			},
			requirement:   gatewayTargetRequirePreviewer,
			wantOutcome:   gatewayTargetNoop,
			wantGatewayID: "app-1",
		},
		{
			name: "runtime requirement reports gateway not running when runtime is missing",
			workers: map[string]testGatewayWorkerState{
				"app-1": {previewer: true},
			},
			target: eventcontract.TargetRef{
				GatewayID:       "app-1",
				SelectionPolicy: eventcontract.GatewaySelectionExplicitOnly,
				FailurePolicy:   eventcontract.GatewayFailureTypedError,
			},
			requirement:   gatewayTargetRequireRuntime,
			wantOutcome:   gatewayTargetGatewayNotRunning,
			wantGatewayID: "app-1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := newTargetResolverTestController(tt.workers)
			got := controller.resolveGatewayTarget(tt.target, tt.requirement)
			if got.Outcome != tt.wantOutcome {
				t.Fatalf("unexpected outcome: got=%s want=%s", got.Outcome, tt.wantOutcome)
			}
			if got.GatewayID != tt.wantGatewayID {
				t.Fatalf("unexpected gateway id: got=%q want=%q", got.GatewayID, tt.wantGatewayID)
			}
			if tt.wantOutcome == gatewayTargetResolved && got.Worker == nil {
				t.Fatal("expected resolved worker, got nil")
			}
		})
	}
}

func TestMultiGatewayControllerApplyFallsBackToSoleGateway(t *testing.T) {
	controller, runtimes, _ := startGatewayControllerForTest(t, []string{"app-1"}, false)

	err := controller.Apply(context.Background(), []Operation{
		{Kind: OperationSendText, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(runtimes["app-1"].applyCalls) != 1 {
		t.Fatalf("unexpected apply calls: %#v", runtimes["app-1"].applyCalls)
	}
	if got := runtimes["app-1"].applyCalls[0][0].Text; got != "hello" {
		t.Fatalf("unexpected operation text: %q", got)
	}
}

func TestMultiGatewayControllerSendIMFileFallsBackToSoleGateway(t *testing.T) {
	controller, runtimes, _ := startGatewayControllerForTest(t, []string{"app-1"}, false)

	result, err := controller.SendIMFile(context.Background(), IMFileSendRequest{
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/report.pdf",
	})
	if err != nil {
		t.Fatalf("SendIMFile: %v", err)
	}
	if result.GatewayID != "app-1" {
		t.Fatalf("unexpected result gateway: %#v", result)
	}
	if len(runtimes["app-1"].sendIMFileCalls) != 1 {
		t.Fatalf("unexpected send file calls: %#v", runtimes["app-1"].sendIMFileCalls)
	}
	if got := runtimes["app-1"].sendIMFileCalls[0].GatewayID; got != "app-1" {
		t.Fatalf("expected resolved gateway id in runtime request, got %q", got)
	}
}

func TestMultiGatewayControllerSendIMImageReturnsTypedErrorWhenTargetIsAmbiguous(t *testing.T) {
	controller, _, _ := startGatewayControllerForTest(t, []string{"app-1", "app-2"}, false)

	_, err := controller.SendIMImage(context.Background(), IMImageSendRequest{
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/preview.png",
	})
	if err == nil {
		t.Fatal("expected typed image send error")
	}
	var sendErr *IMImageSendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("expected IMImageSendError, got %T", err)
	}
	if sendErr.Code != IMImageSendErrorGatewayNotRunning {
		t.Fatalf("unexpected error code: %s", sendErr.Code)
	}
	if got := sendErr.Error(); got != "send image failed: missing gateway id" {
		t.Fatalf("unexpected error text: %q", got)
	}
}

func TestMultiGatewayControllerSendIMVideoFallsBackToSoleGateway(t *testing.T) {
	controller, runtimes, _ := startGatewayControllerForTest(t, []string{"app-1"}, false)

	result, err := controller.SendIMVideo(context.Background(), IMVideoSendRequest{
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/demo.mp4",
	})
	if err != nil {
		t.Fatalf("SendIMVideo: %v", err)
	}
	if result.GatewayID != "app-1" {
		t.Fatalf("unexpected result gateway: %#v", result)
	}
	if len(runtimes["app-1"].sendIMVideoCalls) != 1 {
		t.Fatalf("unexpected send video calls: %#v", runtimes["app-1"].sendIMVideoCalls)
	}
	if got := runtimes["app-1"].sendIMVideoCalls[0].GatewayID; got != "app-1" {
		t.Fatalf("expected resolved gateway id in runtime request, got %q", got)
	}
}

func TestMultiGatewayControllerSendIMVideoReturnsTypedErrorWhenTargetIsAmbiguous(t *testing.T) {
	controller, _, _ := startGatewayControllerForTest(t, []string{"app-1", "app-2"}, false)

	_, err := controller.SendIMVideo(context.Background(), IMVideoSendRequest{
		SurfaceSessionID: "surface-2",
		ChatID:           "oc_2",
		Path:             "/tmp/demo.mp4",
	})
	if err == nil {
		t.Fatal("expected typed video send error")
	}
	var sendErr *IMVideoSendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("expected IMVideoSendError, got %T", err)
	}
	if sendErr.Code != IMVideoSendErrorGatewayNotRunning {
		t.Fatalf("unexpected error code: %s", sendErr.Code)
	}
	if got := sendErr.Error(); got != "send video failed: missing gateway id" {
		t.Fatalf("unexpected error text: %q", got)
	}
}

func TestMultiGatewayControllerRewriteFinalBlockFallsBackToSurfaceGateway(t *testing.T) {
	controller, _, previewers := startGatewayControllerForTest(t, []string{"app-1", "app-2"}, true)

	result, err := controller.RewriteFinalBlock(context.Background(), previewpkg.FinalBlockPreviewRequest{
		SurfaceSessionID: "feishu:app-2:user:user-1",
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

func newTargetResolverTestController(states map[string]testGatewayWorkerState) *MultiGatewayController {
	controller := NewMultiGatewayController()
	controller.workers = map[string]*gatewayWorker{}
	for gatewayID, state := range states {
		worker := &gatewayWorker{
			config: GatewayAppConfig{GatewayID: gatewayID},
		}
		if state.runtime {
			worker.runtime = newFakeGatewayRuntime(gatewayID)
		}
		if state.previewer {
			worker.previewer = &fakePreviewer{gatewayID: gatewayID}
		}
		controller.workers[gatewayID] = worker
	}
	return controller
}

func startGatewayControllerForTest(t *testing.T, gatewayIDs []string, withPreviewers bool) (*MultiGatewayController, map[string]*fakeGatewayRuntime, map[string]*fakePreviewer) {
	t.Helper()

	controller := NewMultiGatewayController()
	runtimes := map[string]*fakeGatewayRuntime{}
	previewers := map[string]*fakePreviewer{}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		runtime := newFakeGatewayRuntime(cfg.GatewayID)
		runtimes[cfg.GatewayID] = runtime
		return runtime
	}
	if withPreviewers {
		controller.newPreviewer = func(_ gatewayRuntime, cfg GatewayAppConfig) gatewayPreviewRuntime {
			previewer := &fakePreviewer{gatewayID: cfg.GatewayID}
			previewers[cfg.GatewayID] = previewer
			return previewer
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	for _, gatewayID := range gatewayIDs {
		if err := controller.UpsertApp(ctx, GatewayAppConfig{
			GatewayID: gatewayID,
			AppID:     "cli_" + gatewayID,
			AppSecret: "secret_" + gatewayID,
			Enabled:   true,
		}); err != nil {
			cancel()
			t.Fatalf("UpsertApp(%s): %v", gatewayID, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx, func(context.Context, control.Action) *ActionResult { return nil })
	}()
	for _, gatewayID := range gatewayIDs {
		waitFakeGatewayStarted(t, waitForFakeRuntime(t, runtimes, gatewayID))
	}

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Start returned error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("timed out waiting for controller stop")
		}
	})

	return controller, runtimes, previewers
}
