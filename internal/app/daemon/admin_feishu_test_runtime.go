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
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

func errFeishuAppNotFound(gatewayID string) error {
	return fmt.Errorf("feishu_app_not_found:%s", strings.TrimSpace(gatewayID))
}

func errFeishuAppRuntimeUnavailable(gatewayID string) error {
	return fmt.Errorf("feishu_app_runtime_unavailable:%s", strings.TrimSpace(gatewayID))
}

func errFeishuAppWebTestRecipientUnavailable(gatewayID string) error {
	return fmt.Errorf("feishu_app_web_test_recipient_unavailable:%s", strings.TrimSpace(gatewayID))
}

func (a *App) handleFeishuAppTestEvents(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppTestStart(w, r, feishuAppTestKindEventSubscription)
}

func (a *App) handleFeishuAppTestCallback(w http.ResponseWriter, r *http.Request) {
	a.handleFeishuAppTestStart(w, r, feishuAppTestKindCallback)
}

func (a *App) handleFeishuAppInstallTestClear(w http.ResponseWriter, r *http.Request) {
	gatewayID := canonicalGatewayID(r.PathValue("id"))
	kind, ok := parseFeishuAppTestKind(r.PathValue("kind"))
	if !ok {
		writeAPIError(w, http.StatusBadRequest, apiError{
			Code:    "invalid_feishu_app_test_kind",
			Message: "unknown feishu app test kind",
			Details: r.PathValue("kind"),
		})
		return
	}
	a.clearFeishuAppTest(gatewayID, kind)
	w.WriteHeader(http.StatusNoContent)
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
		case strings.HasPrefix(err.Error(), "feishu_app_web_test_recipient_unavailable:"):
			writeAPIError(w, http.StatusConflict, apiError{
				Code:    "feishu_app_web_test_recipient_unavailable",
				Message: "the current feishu app does not have a bound web test recipient",
				Details: "手动添加的机器人无法自动发送测试消息，请直接在飞书后台继续手动配置。",
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

	recipient, ok := a.resolveFeishuAppWebTestRecipient(gatewayID)
	if !ok {
		return feishuAppTestStartResponse{}, errFeishuAppWebTestRecipientUnavailable(gatewayID)
	}
	test := a.beginFeishuAppTest(gatewayID, kind, recipient)

	var (
		ops     []feishu.Operation
		message string
	)
	switch kind {
	case feishuAppTestKindCallback:
		ops = []feishu.Operation{feishuAppCallbackTestOperation(summary, recipient, a.daemonLifecycleID)}
		message = "回调测试提示已发送。"
	default:
		ops = []feishu.Operation{feishuAppEventSubscriptionTestOperation(summary, test)}
		message = "事件订阅测试提示已发送。"
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

func feishuAppEventSubscriptionTestOperation(app adminFeishuAppSummary, test feishuAppTestContext) feishu.Operation {
	events := feishuEventRequirementLines(feishuapp.DefaultManifest().Events)
	return feishu.Operation{
		Kind:            feishu.OperationSendCard,
		GatewayID:       app.ID,
		ReceiveID:       test.Recipient.ReceiveID,
		ReceiveIDType:   test.Recipient.ReceiveIDType,
		AttentionUserID: feishuAppTestAttentionUserID(test.Recipient),
		CardTitle:       "事件订阅测试",
		CardThemeKey:    "info",
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": fmt.Sprintf("我们开始测试飞书事件订阅，请确保机器人在 [飞书后台](%s) 配置订阅方式为长连接，并添加以下事件：", strings.TrimSpace(app.ConsoleLinks.Events)),
			},
			{
				"tag": "div",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": strings.Join(events, "\n"),
				},
			},
			{
				"tag": "div",
				"text": map[string]any{
					"tag": "plain_text",
					"content": fmt.Sprintf(
						"请在这里回复“%s”，保证我能收到消息。\n如果我没有回应，请去飞书后台确认增加事件配置以后是否发布了新版本。需要发布以后才会生效。",
						strings.TrimSpace(test.Phrase),
					),
				},
			},
		},
	}
}

func feishuAppCallbackTestOperation(app adminFeishuAppSummary, recipient feishuAppWebTestRecipient, daemonLifecycleID string) feishu.Operation {
	callbacks := feishuCallbackRequirementLines(feishuapp.DefaultManifest().Callbacks)
	value := frontstagecontract.ActionPayloadWithLifecycle(
		frontstagecontract.ActionPayloadPageAction(string(control.ActionFeishuAppTestCallback), ""),
		daemonLifecycleID,
	)
	return feishu.Operation{
		Kind:            feishu.OperationSendCard,
		GatewayID:       app.ID,
		ReceiveID:       recipient.ReceiveID,
		ReceiveIDType:   recipient.ReceiveIDType,
		AttentionUserID: feishuAppTestAttentionUserID(recipient),
		CardTitle:       "回调测试",
		CardThemeKey:    "info",
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": fmt.Sprintf("我们开始测试飞书回调配置，请确保机器人在 [飞书后台](%s) 配置回调订阅方式为长连接，并添加以下回调：", strings.TrimSpace(app.ConsoleLinks.Callback)),
			},
			{
				"tag": "div",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": strings.Join(callbacks, "\n"),
				},
			},
			{
				"tag": "div",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": "请点击下方按钮完成验证。\n如果没有响应，请去飞书后台确认增加回调配置以后是否发布了新版本。需要发布以后才会生效。",
				},
			},
			{
				"tag":  "button",
				"type": "primary",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": "点此测试回调",
				},
				"behaviors": []map[string]any{{
					"type":  "callback",
					"value": value,
				}},
			},
		},
	}
}

func feishuAppTestAttentionUserID(recipient feishuAppWebTestRecipient) string {
	if strings.TrimSpace(recipient.ActorUserID) != "" {
		return strings.TrimSpace(recipient.ActorUserID)
	}
	return strings.TrimSpace(recipient.ReceiveID)
}

func (a *App) beginFeishuAppTest(gatewayID string, kind feishuAppTestKind, recipient feishuAppWebTestRecipient) feishuAppTestContext {
	now := time.Now().UTC()
	expiresAt := now.Add(defaultFeishuAppTestTTL)
	id, err := randomHex(8)
	if err != nil {
		id = strings.ReplaceAll(now.Format("20060102150405"), " ", "")
	}
	record := &feishuAppTestContext{
		ID:        id,
		GatewayID: canonicalGatewayID(gatewayID),
		Kind:      kind,
		Phrase:    defaultFeishuAppEventTestPhrase,
		Recipient: recipient,
		Status:    feishuAppTestStatusPending,
		StartedAt: now,
		ExpiresAt: expiresAt,
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
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, feishuAppTestKindEventSubscription))
	delete(a.feishuRuntime.tests, feishuAppTestKey(gatewayID, feishuAppTestKindCallback))
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

func (a *App) bindFeishuAppWebTestRecipient(gatewayID, actorUserID string) {
	gatewayID = canonicalGatewayID(gatewayID)
	actorUserID = strings.TrimSpace(actorUserID)
	receiveID, receiveIDType := feishu.ResolveReceiveTarget("", actorUserID)
	if gatewayID == "" || receiveID == "" || receiveIDType == "" {
		return
	}
	recipient := feishuAppWebTestRecipient{
		GatewayID:     gatewayID,
		ActorUserID:   actorUserID,
		ReceiveID:     receiveID,
		ReceiveIDType: receiveIDType,
		BoundAt:       time.Now().UTC(),
	}
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	if a.feishuRuntime.webTestRecipients == nil {
		a.feishuRuntime.webTestRecipients = map[string]feishuAppWebTestRecipient{}
	}
	a.feishuRuntime.webTestRecipients[gatewayID] = recipient
}

func (a *App) clearFeishuAppWebTestRecipient(gatewayID string) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	delete(a.feishuRuntime.webTestRecipients, canonicalGatewayID(gatewayID))
}

func (a *App) resolveFeishuAppWebTestRecipient(gatewayID string) (feishuAppWebTestRecipient, bool) {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	a.cleanupFeishuAppTestsLocked(time.Now().UTC())
	recipient, ok := a.feishuRuntime.webTestRecipients[canonicalGatewayID(gatewayID)]
	if !ok || recipient.ReceiveID == "" || recipient.ReceiveIDType == "" {
		return feishuAppWebTestRecipient{}, false
	}
	return recipient, true
}

func feishuAppTestKey(gatewayID string, kind feishuAppTestKind) string {
	return canonicalGatewayID(gatewayID) + "|" + strings.TrimSpace(string(kind))
}

func parseFeishuAppTestKind(value string) (feishuAppTestKind, bool) {
	switch strings.TrimSpace(value) {
	case "events", string(feishuAppTestKindEventSubscription):
		return feishuAppTestKindEventSubscription, true
	case string(feishuAppTestKindCallback):
		return feishuAppTestKindCallback, true
	default:
		return "", false
	}
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

func (a *App) maybeHandleFeishuAppTestActionLocked(ctx context.Context, action control.Action) bool {
	switch action.Kind {
	case control.ActionTextMessage:
		if strings.TrimSpace(action.Text) != defaultFeishuAppEventTestPhrase {
			return false
		}
		message := "当前没有进行中的事件订阅测试，请回到配置页面重新发起。"
		if a.markFeishuAppTestPassedLocked(action, feishuAppTestKindEventSubscription) {
			message = "测试成功，请回到配置页面继续下一步工作。"
		}
		a.replyToFeishuAppTestActionLocked(ctx, action, message)
		return true
	case control.ActionFeishuAppTestCallback:
		message := "当前没有进行中的回调测试，请回到配置页面重新发起。"
		if a.markFeishuAppTestPassedLocked(action, feishuAppTestKindCallback) {
			message = "回调测试成功，请回到配置页面继续下一步工作。"
		}
		a.replyToFeishuAppTestActionLocked(ctx, action, message)
		return true
	default:
		return false
	}
}

func (a *App) markFeishuAppTestPassedLocked(action control.Action, kind feishuAppTestKind) bool {
	a.feishuRuntime.mu.Lock()
	defer a.feishuRuntime.mu.Unlock()
	a.cleanupFeishuAppTestsLocked(time.Now().UTC())
	record := a.feishuRuntime.tests[feishuAppTestKey(action.GatewayID, kind)]
	if record == nil || record.Status != feishuAppTestStatusPending {
		return false
	}
	if strings.TrimSpace(record.Recipient.ActorUserID) != "" &&
		strings.TrimSpace(record.Recipient.ActorUserID) != strings.TrimSpace(action.ActorUserID) {
		return false
	}
	record.Status = feishuAppTestStatusPassed
	delete(a.feishuRuntime.tests, feishuAppTestKey(action.GatewayID, kind))
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
	recipient, ok := a.resolveFeishuAppWebTestRecipient(gatewayID)
	if !ok {
		return
	}
	secondLine := "机器人连接验证已通过，请回到管理页继续。"
	if setupFlow {
		secondLine = "机器人基础设置已通过，请回到 Web Setup 继续。"
	}
	ops := []feishu.Operation{
		{
			Kind:          feishu.OperationSendText,
			GatewayID:     recipient.GatewayID,
			ReceiveID:     recipient.ReceiveID,
			ReceiveIDType: recipient.ReceiveIDType,
			Text:          "连接验证成功。",
		},
		{
			Kind:          feishu.OperationSendText,
			GatewayID:     recipient.GatewayID,
			ReceiveID:     recipient.ReceiveID,
			ReceiveIDType: recipient.ReceiveIDType,
			Text:          secondLine,
		},
	}
	if err := a.applyFeishuOperations(ctx, ops); err != nil {
		log.Printf("feishu verify success notice failed: gateway=%s err=%v", gatewayID, err)
	}
}

func feishuEventRequirementLines(values []feishuapp.EventRequirement) []string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		event := strings.TrimSpace(value.Event)
		purpose := strings.TrimSpace(value.Purpose)
		if event == "" {
			continue
		}
		if purpose == "" {
			lines = append(lines, event)
			continue
		}
		lines = append(lines, event+"："+purpose)
	}
	return lines
}

func feishuCallbackRequirementLines(values []feishuapp.CallbackRequirement) []string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		callback := strings.TrimSpace(value.Callback)
		purpose := strings.TrimSpace(value.Purpose)
		if callback == "" {
			continue
		}
		if purpose == "" {
			lines = append(lines, callback)
			continue
		}
		lines = append(lines, callback+"："+purpose)
	}
	return lines
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
