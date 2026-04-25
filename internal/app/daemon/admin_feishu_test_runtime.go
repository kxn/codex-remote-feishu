package daemon

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func errFeishuAppNotFound(gatewayID string) error {
	return fmt.Errorf("feishu_app_not_found:%s", strings.TrimSpace(gatewayID))
}

func errFeishuAppRuntimeUnavailable(gatewayID string) error {
	return fmt.Errorf("feishu_app_runtime_unavailable:%s", strings.TrimSpace(gatewayID))
}

func errFeishuAppTestTargetUnavailable(gatewayID string) error {
	return fmt.Errorf("feishu_app_test_target_unavailable:%s", strings.TrimSpace(gatewayID))
}

func (a *App) handleFeishuAppTestEvents(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppTestStart(w, r, feishuAppTestKindEventSubscription)
}

func (a *App) handleFeishuAppTestCallback(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppTestStart(w, r, feishuAppTestKindCallback)
}

func (a *App) handleFeishuAppTestStart(w http.ResponseWriter, r *http.Request, kind feishuAppTestKind) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	resp, err := a.startFeishuAppTest(r.Context(), gatewayID, kind)
	if err != nil {
		switch {
		case strings.HasPrefix(err.Error(), "feishu_app_not_found:"):
			writeAPIError(w, http.StatusNotFound, apiError{
				Code:    "feishu_app_not_found",
				Message: "feishu app not found",
				Details: gatewayID,
			})
		case strings.HasPrefix(err.Error(), "feishu_app_runtime_unavailable:"):
			writeAPIError(w, http.StatusConflict, apiError{
				Code:    "feishu_app_runtime_unavailable",
				Message: "feishu app is not available at runtime",
				Details: gatewayID,
			})
		case strings.HasPrefix(err.Error(), "feishu_app_test_target_unavailable:"):
			writeAPIError(w, http.StatusConflict, apiError{
				Code:    "feishu_app_test_target_unavailable",
				Message: "no recent Feishu conversation is available for this app yet",
				Details: "请先在飞书里给这个机器人发送一条消息，再回到网页重试。",
			})
		default:
			writeAPIError(w, http.StatusBadGateway, apiError{
				Code:    "feishu_app_test_start_failed",
				Message: "failed to start feishu app test",
				Details: err.Error(),
			})
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) startFeishuAppTest(ctx context.Context, gatewayID string, kind feishuAppTestKind) (feishuAppTestStartResponse, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return feishuAppTestStartResponse{}, err
	}
	summary, ok, err := a.adminFeishuAppSummary(loaded, gatewayID)
	if err != nil {
		return feishuAppTestStartResponse{}, err
	}
	if !ok {
		return feishuAppTestStartResponse{}, errFeishuAppNotFound(gatewayID)
	}
	if _, ok := a.runtimeGatewayConfigFor(loaded.Config, gatewayID); !ok {
		return feishuAppTestStartResponse{}, errFeishuAppRuntimeUnavailable(gatewayID)
	}

	target, ok := a.resolveFeishuAppTestDeliveryTarget(gatewayID)
	if !ok {
		return feishuAppTestStartResponse{}, errFeishuAppTestTargetUnavailable(gatewayID)
	}
	test := a.beginFeishuAppTest(gatewayID, kind, target.SurfaceSessionID)

	var (
		ops     []feishu.Operation
		message string
	)
	switch kind {
	case feishuAppTestKindCallback:
		ops = []feishu.Operation{feishuAppCallbackTestOperation(target, a.daemonLifecycleID)}
		message = "测试卡片已发往最近使用该机器人的飞书会话。"
	default:
		ops = []feishu.Operation{feishuAppEventSubscriptionTestOperation(target, test.Phrase)}
		message = "测试提示已发往最近使用该机器人的飞书会话。"
	}

	if err := a.applyFeishuOperations(ctx, ops); err != nil {
		a.clearFeishuAppTest(gatewayID, kind)
		return feishuAppTestStartResponse{}, err
	}
	resp := feishuAppTestStartResponse{
		GatewayID: summary.ID,
		StartedAt: test.StartedAt,
		ExpiresAt: test.ExpiresAt,
		Message:   message,
	}
	if kind == feishuAppTestKindEventSubscription {
		resp.Phrase = test.Phrase
	}
	return resp, nil
}

func feishuAppEventSubscriptionTestOperation(target feishuAppTestDeliveryTarget, phrase string) feishu.Operation {
	return feishu.Operation{
		Kind:             feishu.OperationSendText,
		GatewayID:        target.GatewayID,
		SurfaceSessionID: target.SurfaceSessionID,
		ChatID:           target.ChatID,
		ReceiveID:        target.ReceiveID,
		ReceiveIDType:    target.ReceiveIDType,
		Text:             fmt.Sprintf("请直接回复“%s”完成事件订阅测试。如果没有收到回复，请回到飞书后台检查事件订阅配置。", strings.TrimSpace(phrase)),
	}
}

func feishuAppCallbackTestOperation(target feishuAppTestDeliveryTarget, daemonLifecycleID string) feishu.Operation {
	value := frontstagecontract.ActionPayloadWithLifecycle(
		frontstagecontract.ActionPayloadPageAction(string(control.ActionFeishuAppTestCallback), ""),
		daemonLifecycleID,
	)
	return feishu.Operation{
		Kind:             feishu.OperationSendCard,
		GatewayID:        target.GatewayID,
		SurfaceSessionID: target.SurfaceSessionID,
		ChatID:           target.ChatID,
		ReceiveID:        target.ReceiveID,
		ReceiveIDType:    target.ReceiveIDType,
		CardTitle:        "回调测试",
		CardThemeKey:     "info",
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": "请点击下方按钮测试回调能力。如果点击后没有看到回复，请回到飞书后台检查回调配置。",
			},
			{
				"tag": "action",
				"actions": []map[string]any{
					{
						"tag":  "button",
						"type": "primary",
						"text": map[string]any{
							"tag":     "plain_text",
							"content": "点此测试回调",
						},
						"value": value,
					},
				},
			},
		},
	}
}

func (a *App) beginFeishuAppTest(gatewayID string, kind feishuAppTestKind, surfaceSessionID string) feishuAppTestContext {
	now := time.Now().UTC()
	expiresAt := now.Add(defaultFeishuAppTestTTL)
	id, err := randomHex(8)
	if err != nil {
		id = strings.ReplaceAll(now.Format("20060102150405"), " ", "")
	}
	record := &feishuAppTestContext{
		ID:               id,
		GatewayID:        canonicalGatewayID(gatewayID),
		Kind:             kind,
		Phrase:           defaultFeishuAppEventTestPhrase,
		SurfaceSessionID: strings.TrimSpace(surfaceSessionID),
		Status:           feishuAppTestStatusPending,
		StartedAt:        now,
		ExpiresAt:        expiresAt,
	}
	if kind != feishuAppTestKindEventSubscription {
		record.Phrase = ""
	}
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	a.cleanupFeishuAppTestsLocked(now)
	if a.feishuRuntime.tests == nil {
		a.feishuRuntime.tests = map[string]*feishuAppTestContext{}
	}
	a.feishuRuntime.tests[feishuAppTestKey(gatewayID, kind)] = record
	return *record
}

func (a *App) clearFeishuAppTest(gatewayID string, kind feishuAppTestKind) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, kind))
}

func (a *App) clearAllFeishuAppTests(gatewayID string) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, feishuAppTestKindEventSubscription))
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, feishuAppTestKindCallback))
}

func feishuAppTestKey(gatewayID string, kind feishuAppTestKind) string {
	return canonicalGatewayID(gatewayID) + "|" + strings.TrimSpace(string(kind))
}

func (a *App) cleanupFeishuAppTestsLocked(now time.Time) {
	if len(a.feishuRuntime.tests) == 0 {
		return
	}
	for key, record := range a.feishuRuntime.tests {
		if record == nil {
			delete(a.feishuRuntime.tests, key)
			continue
		}
		if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
			record.Status = feishuAppTestStatusExpired
			delete(a.feishuRuntime.tests, key)
		}
	}
}

func (a *App) resolveFeishuAppTestDeliveryTarget(gatewayID string) (feishuAppTestDeliveryTarget, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolveFeishuAppTestDeliveryTargetLocked(gatewayID)
}

func (a *App) resolveFeishuAppTestDeliveryTargetLocked(gatewayID string) (feishuAppTestDeliveryTarget, bool) {
	gatewayID = canonicalGatewayID(gatewayID)
	if gatewayID == "" {
		return feishuAppTestDeliveryTarget{}, false
	}
	var (
		best      feishuAppTestDeliveryTarget
		bestAt    time.Time
		bestFound bool
	)
	for _, surface := range a.service.Surfaces() {
		if surface == nil || canonicalGatewayID(surface.GatewayID) != gatewayID {
			continue
		}
		receiveID, receiveIDType := feishu.ResolveReceiveTarget(surface.ChatID, surface.ActorUserID)
		if receiveID == "" || receiveIDType == "" {
			continue
		}
		candidate := feishuAppTestDeliveryTarget{
			GatewayID:        gatewayID,
			SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
			ChatID:           strings.TrimSpace(surface.ChatID),
			ActorUserID:      strings.TrimSpace(surface.ActorUserID),
			ReceiveID:        receiveID,
			ReceiveIDType:    receiveIDType,
		}
		if !bestFound || surface.LastInboundAt.After(bestAt) {
			best = candidate
			bestAt = surface.LastInboundAt
			bestFound = true
		}
	}
	return best, bestFound
}

func (a *App) maybeHandleFeishuAppTestActionLocked(ctx context.Context, action control.Action) bool {
	switch action.Kind {
	case control.ActionTextMessage:
		if strings.TrimSpace(action.Text) != defaultFeishuAppEventTestPhrase {
			return false
		}
		message := "当前没有进行中的事件订阅测试，请回到网页重新发起。"
		if a.markFeishuAppTestPassedLocked(action.GatewayID, feishuAppTestKindEventSubscription) {
			message = "事件订阅测试已通过，请回到网页继续。"
		}
		a.replyToFeishuAppTestActionLocked(ctx, action, message)
		return true
	case control.ActionFeishuAppTestCallback:
		message := "当前没有进行中的回调测试，请回到网页重新发起。"
		if a.markFeishuAppTestPassedLocked(action.GatewayID, feishuAppTestKindCallback) {
			message = "回调测试已通过，请回到网页继续。"
		}
		a.replyToFeishuAppTestActionLocked(ctx, action, message)
		return true
	default:
		return false
	}
}

func (a *App) markFeishuAppTestPassedLocked(gatewayID string, kind feishuAppTestKind) bool {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	a.cleanupFeishuAppTestsLocked(time.Now().UTC())
	record := a.feishuRuntime.tests[feishuAppTestKey(gatewayID, kind)]
	if record == nil || record.Status != feishuAppTestStatusPending {
		return false
	}
	record.Status = feishuAppTestStatusPassed
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, kind))
	return true
}

func (a *App) replyToFeishuAppTestActionLocked(ctx context.Context, action control.Action, text string) {
	receiveID, receiveIDType := feishu.ResolveReceiveTarget(action.ChatID, action.ActorUserID)
	op := feishu.Operation{
		Kind:             feishu.OperationSendText,
		GatewayID:        canonicalGatewayID(action.GatewayID),
		SurfaceSessionID: strings.TrimSpace(action.SurfaceSessionID),
		ChatID:           strings.TrimSpace(action.ChatID),
		ReceiveID:        receiveID,
		ReceiveIDType:    receiveIDType,
		ReplyToMessageID: strings.TrimSpace(action.MessageID),
		Text:             strings.TrimSpace(text),
	}
	if err := a.applyFeishuOperationsUnlockedLocked(ctx, []feishu.Operation{op}); err != nil {
		log.Printf("feishu app test reply failed: gateway=%s surface=%s kind=%s err=%v", action.GatewayID, action.SurfaceSessionID, action.Kind, err)
	}
}

func (a *App) maybeSendFeishuAppVerifySuccessNotices(ctx context.Context, gatewayID string, setupFlow bool) {
	target, ok := a.resolveFeishuAppTestDeliveryTarget(gatewayID)
	if !ok {
		return
	}
	secondLine := "机器人连接验证已通过，请回到管理页继续。"
	if setupFlow {
		secondLine = "机器人基础设置已通过，请回到 Web Setup 继续。"
	}
	ops := []feishu.Operation{
		{
			Kind:             feishu.OperationSendText,
			GatewayID:        target.GatewayID,
			SurfaceSessionID: target.SurfaceSessionID,
			ChatID:           target.ChatID,
			ReceiveID:        target.ReceiveID,
			ReceiveIDType:    target.ReceiveIDType,
			Text:             "连接验证成功。",
		},
		{
			Kind:             feishu.OperationSendText,
			GatewayID:        target.GatewayID,
			SurfaceSessionID: target.SurfaceSessionID,
			ChatID:           target.ChatID,
			ReceiveID:        target.ReceiveID,
			ReceiveIDType:    target.ReceiveIDType,
			Text:             secondLine,
		},
	}
	if err := a.applyFeishuOperations(ctx, ops); err != nil {
		log.Printf("feishu verify success notice failed: gateway=%s err=%v", gatewayID, err)
	}
}

func (a *App) applyFeishuOperations(ctx context.Context, operations []feishu.Operation) error {
	if len(operations) == 0 {
		return nil
	}
	sendCtx, cancel := a.newTimeoutContext(ctx, defaultFeishuAppTestSendTimeout)
	defer cancel()
	err := a.gateway.Apply(sendCtx, operations)
	if err != nil {
		for _, op := range operations {
			if observed := a.observeFeishuPermissionError(op.GatewayID, err); observed {
				break
			}
		}
	}
	return err
}

func (a *App) applyFeishuOperationsUnlockedLocked(ctx context.Context, operations []feishu.Operation) error {
	a.mu.Unlock()
	err := a.applyFeishuOperations(ctx, operations)
	a.mu.Lock()
	return err
}
