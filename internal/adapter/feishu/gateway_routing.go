package feishu

import (
	"fmt"
	"strconv"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (g *LiveGateway) parseCardActionTriggerEvent(event *larkcallback.CardActionTriggerEvent) (control.Action, bool) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return control.Action{}, false
	}
	meta := inboundMetaFromCardActionEvent(event)
	value := event.Event.Action.Value
	kind := actionPayloadKind(value)
	if kind == "" {
		return control.Action{}, false
	}

	operatorID := operatorUserIDFromCard(event.Event.Operator)
	chatID := ""
	messageID := ""
	if event.Event.Context != nil {
		chatID = strings.TrimSpace(event.Event.Context.OpenChatID)
		messageID = strings.TrimSpace(event.Event.Context.OpenMessageID)
	}
	surfaceSessionID := g.surfaceForCardAction(messageID, chatID, operatorID)
	if surfaceSessionID == "" {
		return control.Action{}, false
	}

	switch kind {
	case cardActionKindAttachInstance:
		instanceID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyInstanceID))
		if instanceID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionAttachInstance,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			InstanceID:       instanceID,
			Inbound:          meta,
		}, true
	case cardActionKindAttachWorkspace:
		workspaceKey := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyWorkspaceKey))
		if workspaceKey == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionAttachWorkspace,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			WorkspaceKey:     workspaceKey,
			Inbound:          meta,
		}, true
	case cardActionKindCreateWorkspace:
		return control.Action{
			Kind:             control.ActionCreateWorkspace,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Inbound:          meta,
		}, true
	case cardActionKindUseThread:
		threadID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyThreadID))
		if threadID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:                control.ActionUseThread,
			GatewayID:           g.config.GatewayID,
			SurfaceSessionID:    surfaceSessionID,
			ChatID:              chatID,
			ActorUserID:         operatorID,
			MessageID:           messageID,
			ThreadID:            threadID,
			AllowCrossWorkspace: boolMapValue(value, cardActionPayloadKeyAllowCrossWorkspace),
			Inbound:             meta,
		}, true
	case cardActionKindShowScopedThreads:
		return control.Action{
			Kind:             control.ActionShowScopedThreads,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ViewMode:         strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyViewMode)),
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowThreads:
		return control.Action{
			Kind:             control.ActionShowThreads,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ViewMode:         strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyViewMode)),
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowAllThreads:
		return control.Action{
			Kind:             control.ActionShowAllThreads,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ViewMode:         strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyViewMode)),
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowAllThreadWorkspaces:
		return control.Action{
			Kind:             control.ActionShowAllThreadWorkspaces,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowRecentThreadWorkspaces:
		return control.Action{
			Kind:             control.ActionShowRecentThreadWorkspaces,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowWorkspaceThreads:
		workspaceKey := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyWorkspaceKey))
		if workspaceKey == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionShowWorkspaceThreads,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			WorkspaceKey:     workspaceKey,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			ReturnPage:       intMapValue(value, cardActionPayloadKeyReturnPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowAllWorkspaces:
		return control.Action{
			Kind:             control.ActionShowAllWorkspaces,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindShowRecentWorkspaces:
		return control.Action{
			Kind:             control.ActionShowRecentWorkspaces,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindResumeHeadlessThread:
		return control.Action{
			Kind:             control.ActionRemovedCommand,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Text:             "resume_headless_thread",
			Inbound:          meta,
		}, true
	case cardActionKindKickThreadConfirm:
		threadID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyThreadID))
		if threadID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionConfirmKickThread,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			ThreadID:         threadID,
			Inbound:          meta,
		}, true
	case cardActionKindKickThreadCancel:
		return control.Action{
			Kind:             control.ActionCancelKickThread,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			Inbound:          meta,
		}, true
	case cardActionKindPromptSelect:
		promptID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPromptID))
		optionID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyOptionID))
		if promptID == "" || optionID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionSelectPrompt,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PromptID:         promptID,
			OptionID:         optionID,
			Inbound:          meta,
		}, true
	case cardActionKindRequestRespond:
		requestID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestID))
		if requestID == "" {
			return control.Action{}, false
		}
		requestAnswers := requestAnswersFromValue(value)
		optionID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestOptionID))
		if optionID == "" && len(requestAnswers) == 0 {
			if value[cardActionPayloadKeyApproved] != nil {
				if boolMapValue(value, cardActionPayloadKeyApproved) {
					optionID = "accept"
				} else {
					optionID = "decline"
				}
			}
		}
		return control.Action{
			Kind:             control.ActionRespondRequest,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			RequestID:        requestID,
			RequestType:      strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestType)),
			RequestOptionID:  optionID,
			RequestAnswers:   requestAnswers,
			RequestRevision:  intMapValue(value, cardActionPayloadKeyRequestRevision),
			Approved:         boolMapValue(value, cardActionPayloadKeyApproved),
			Inbound:          meta,
		}, true
	case cardActionKindRunCommand:
		commandText := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandText))
		if commandText == "" {
			commandText = strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandLegacy))
		}
		action, ok := parseTextAction(commandText)
		if !ok {
			return control.Action{}, false
		}
		action.GatewayID = g.config.GatewayID
		action.SurfaceSessionID = surfaceSessionID
		action.ChatID = chatID
		action.ActorUserID = operatorID
		action.MessageID = messageID
		action.Inbound = meta
		return action, true
	case cardActionKindStartCommandCapture:
		commandID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandID))
		if commandID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionStartCommandCapture,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			CommandID:        commandID,
			Inbound:          meta,
		}, true
	case cardActionKindCancelCommandCapture:
		commandID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandID))
		if commandID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionCancelCommandCapture,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			CommandID:        commandID,
			Inbound:          meta,
		}, true
	case cardActionKindSubmitCommandForm:
		commandText := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandLegacy))
		if commandText == "" {
			commandText = strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyCommandText))
		}
		if commandText == "" {
			return control.Action{}, false
		}
		fieldName := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyFieldName))
		if fieldName == "" {
			fieldName = cardActionPayloadDefaultCommandFieldName
		}
		args := strings.TrimSpace(formStringValue(event.Event.Action.FormValue, fieldName))
		if args == "" {
			args = strings.TrimSpace(event.Event.Action.InputValue)
		}
		if args != "" {
			commandText += " " + args
		}
		action, ok := parseTextAction(commandText)
		if !ok {
			return control.Action{}, false
		}
		action.GatewayID = g.config.GatewayID
		action.SurfaceSessionID = surfaceSessionID
		action.ChatID = chatID
		action.ActorUserID = operatorID
		action.MessageID = messageID
		action.Inbound = meta
		return action, true
	case cardActionKindSubmitRequestForm:
		requestID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestID))
		if requestID == "" {
			return control.Action{}, false
		}
		requestOptionID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestOptionID))
		requestAnswers := requestAnswersFromFormValue(event.Event.Action.FormValue)
		if len(requestAnswers) == 0 && strings.TrimSpace(event.Event.Action.InputValue) != "" {
			fieldName := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyFieldName))
			if fieldName != "" {
				if requestAnswers == nil {
					requestAnswers = map[string][]string{}
				}
				requestAnswers[fieldName] = []string{strings.TrimSpace(event.Event.Action.InputValue)}
			}
		}
		return control.Action{
			Kind:             control.ActionRespondRequest,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			RequestID:        requestID,
			RequestType:      strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyRequestType)),
			RequestOptionID:  requestOptionID,
			RequestAnswers:   requestAnswers,
			RequestRevision:  intMapValue(value, cardActionPayloadKeyRequestRevision),
			Inbound:          meta,
		}, true
	case cardActionKindPathPickerEnter, cardActionKindPathPickerSelect:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		entryName := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyEntryName))
		if entryName == "" {
			entryName = pathPickerSelectedEntryName(event, strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyFieldName)))
		}
		if pickerID == "" || entryName == "" {
			return control.Action{}, false
		}
		actionKind := control.ActionPathPickerEnter
		if actionPayloadKind(value) == cardActionKindPathPickerSelect {
			actionKind = control.ActionPathPickerSelect
		}
		return control.Action{
			Kind:             actionKind,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			PickerEntry:      entryName,
			Inbound:          meta,
		}, true
	case cardActionKindPathPickerUp, cardActionKindPathPickerConfirm, cardActionKindPathPickerCancel:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		actionKind := control.ActionPathPickerUp
		switch actionPayloadKind(value) {
		case cardActionKindPathPickerConfirm:
			actionKind = control.ActionPathPickerConfirm
		case cardActionKindPathPickerCancel:
			actionKind = control.ActionPathPickerCancel
		}
		return control.Action{
			Kind:             actionKind,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			Inbound:          meta,
		}, true
	case cardActionKindTargetPickerSelectMode,
		cardActionKindTargetPickerSelectSource,
		cardActionKindTargetPickerSelectWorkspace,
		cardActionKindTargetPickerSelectSession,
		cardActionKindTargetPickerOpenPathPicker,
		cardActionKindTargetPickerCancel,
		cardActionKindTargetPickerConfirm:
		return g.parseTargetPickerCardAction(value, event, meta, surfaceSessionID, chatID, operatorID, messageID)
	case cardActionKindHistoryPage:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionHistoryPage,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			Page:             intMapValue(value, cardActionPayloadKeyPage),
			Inbound:          meta,
		}, true
	case cardActionKindHistoryDetail:
		pickerID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyPickerID))
		if pickerID == "" {
			return control.Action{}, false
		}
		turnID := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyTurnID))
		if turnID == "" {
			fieldName := strings.TrimSpace(stringMapValue(value, cardActionPayloadKeyFieldName))
			if fieldName == "" {
				fieldName = cardThreadHistoryTurnFieldName
			}
			turnID = selectStaticFormValue(event.Event.Action.FormValue, fieldName)
			if turnID == "" {
				turnID = pathPickerSelectedEntryName(event, fieldName)
			}
		}
		if turnID == "" {
			return control.Action{}, false
		}
		return control.Action{
			Kind:             control.ActionHistoryDetail,
			GatewayID:        g.config.GatewayID,
			SurfaceSessionID: surfaceSessionID,
			ChatID:           chatID,
			ActorUserID:      operatorID,
			MessageID:        messageID,
			PickerID:         pickerID,
			TurnID:           turnID,
			Inbound:          meta,
		}, true
	default:
		return control.Action{}, false
	}
}

func formStringValue(values map[string]interface{}, key string) string {
	if len(values) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return fmt.Sprint(raw)
	}
}

func pathPickerSelectedEntryName(event *larkcallback.CardActionTriggerEvent, fieldName string) string {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return ""
	}
	action := event.Event.Action
	if option := strings.TrimSpace(action.Option); option != "" {
		return option
	}
	for _, option := range action.Options {
		if option = strings.TrimSpace(option); option != "" {
			return option
		}
	}
	if fieldName != "" {
		return selectStaticFormValue(action.FormValue, fieldName)
	}
	return ""
}

func selectStaticFormValue(values map[string]interface{}, key string) string {
	if len(values) == 0 || strings.TrimSpace(key) == "" {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				return item
			}
		}
	case []interface{}:
		for _, item := range typed {
			if item == nil {
				continue
			}
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				return text
			}
		}
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
	return ""
}

func requestAnswersFromValue(values map[string]interface{}) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	raw, ok := values[cardActionPayloadKeyRequestAnswers]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case map[string]interface{}:
		return requestAnswersFromMap(typed)
	default:
		return nil
	}
}

func requestAnswersFromFormValue(values map[string]interface{}) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	answers := map[string][]string{}
	for key := range values {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		text := strings.TrimSpace(formStringValue(values, key))
		if text == "" {
			continue
		}
		answers[name] = []string{text}
	}
	if len(answers) == 0 {
		return nil
	}
	return answers
}

func targetPickerDraftAnswersFromFormValue(values map[string]interface{}) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	answers := map[string][]string{}
	for _, fieldName := range []string{
		control.FeishuTargetPickerGitRepoURLFieldName,
		control.FeishuTargetPickerGitDirectoryNameFieldName,
	} {
		if _, ok := values[fieldName]; !ok {
			continue
		}
		answers[fieldName] = []string{strings.TrimSpace(formStringValue(values, fieldName))}
	}
	if len(answers) == 0 {
		return nil
	}
	return answers
}

func requestAnswersFromMap(values map[string]interface{}) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	answers := map[string][]string{}
	for key, raw := range values {
		name := strings.TrimSpace(key)
		if name == "" || raw == nil {
			continue
		}
		var out []string
		switch typed := raw.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				out = []string{text}
			}
		case []interface{}:
			for _, item := range typed {
				if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
					out = append(out, text)
				}
			}
		default:
			if text := strings.TrimSpace(fmt.Sprint(typed)); text != "" {
				out = []string{text}
			}
		}
		if len(out) != 0 {
			answers[name] = out
		}
	}
	if len(answers) == 0 {
		return nil
	}
	return answers
}

func (g *LiveGateway) surfaceForCardAction(messageID, chatID, operatorID string) string {
	if messageID != "" {
		g.mu.Lock()
		surfaceSessionID := g.messages[messageID]
		g.mu.Unlock()
		if surfaceSessionID != "" {
			return surfaceSessionID
		}
	}
	if operatorID != "" {
		return surfaceIDForInbound(g.config.GatewayID, "", "p2p", operatorID)
	}
	if chatID != "" {
		return surfaceID(g.config.GatewayID, chatID, "")
	}
	return ""
}

func parseTextAction(text string) (control.Action, bool) {
	return control.ParseFeishuTextAction(text)
}

func menuAction(eventKey string) (control.Action, bool) {
	return control.ParseFeishuMenuAction(eventKey)
}

func normalizeMenuEventKey(value string) string {
	return control.NormalizeFeishuMenuEventKey(value)
}

func menuActionKind(eventKey string) (control.ActionKind, bool) {
	action, ok := menuAction(eventKey)
	if !ok {
		return "", false
	}
	return action.Kind, true
}

func surfaceID(gatewayID, chatID, fallbackUserID string) string {
	if chatID != "" {
		return SurfaceRef{
			Platform:  PlatformFeishu,
			GatewayID: normalizeGatewayID(gatewayID),
			ScopeKind: ScopeKindChat,
			ScopeID:   strings.TrimSpace(chatID),
		}.SurfaceID()
	}
	return SurfaceRef{
		Platform:  PlatformFeishu,
		GatewayID: normalizeGatewayID(gatewayID),
		ScopeKind: ScopeKindUser,
		ScopeID:   strings.TrimSpace(fallbackUserID),
	}.SurfaceID()
}

func surfaceIDForInbound(gatewayID, chatID, chatType, fallbackUserID string) string {
	if strings.EqualFold(chatType, "p2p") && fallbackUserID != "" {
		return surfaceID(gatewayID, "", fallbackUserID)
	}
	return surfaceID(gatewayID, chatID, fallbackUserID)
}

func userIDFromMessage(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	return userIDFromLarkUserID(sender.SenderId)
}

func userIDFromLarkUserID(userID *larkim.UserId) string {
	if userID == nil {
		return ""
	}
	return preferredFeishuUserID(stringPtr(userID.OpenId), stringPtr(userID.UserId), stringPtr(userID.UnionId))
}

func operatorUserID(operator *larkapplication.Operator) string {
	if operator == nil || operator.OperatorId == nil {
		return ""
	}
	return preferredFeishuUserID(
		stringPtr(operator.OperatorId.OpenId),
		stringPtr(operator.OperatorId.UserId),
		stringPtr(operator.OperatorId.UnionId),
	)
}

func operatorUserIDFromCard(operator *larkcallback.Operator) string {
	if operator == nil {
		return ""
	}
	return preferredFeishuUserID(strings.TrimSpace(operator.OpenID), stringPtr(operator.UserID), "")
}

func reactionKey(messageID, emojiType string) string {
	return messageID + "|" + emojiType
}

func mimeExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func chooseFirst(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func preferredFeishuUserID(openID, userID, unionID string) string {
	return chooseFirst(
		strings.TrimSpace(openID),
		strings.TrimSpace(userID),
		strings.TrimSpace(unionID),
	)
}

func stringMapValue(values map[string]interface{}, key string) string {
	if len(values) == 0 {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch current := value.(type) {
	case string:
		return current
	case fmt.Stringer:
		return current.String()
	default:
		return fmt.Sprint(current)
	}
}

func boolMapValue(values map[string]interface{}, key string) bool {
	if len(values) == 0 {
		return false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	current, _ := value.(bool)
	return current
}

func intMapValue(values map[string]interface{}, key string) int {
	if len(values) == 0 {
		return 0
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}
	switch current := value.(type) {
	case int:
		return current
	case int8:
		return int(current)
	case int16:
		return int(current)
	case int32:
		return int(current)
	case int64:
		return int(current)
	case uint:
		return int(current)
	case uint8:
		return int(current)
	case uint16:
		return int(current)
	case uint32:
		return int(current)
	case uint64:
		return int(current)
	case float32:
		return int(current)
	case float64:
		return int(current)
	case string:
		current = strings.TrimSpace(current)
		if current == "" {
			return 0
		}
		parsed, err := strconv.Atoi(current)
		if err == nil {
			return parsed
		}
	default:
		return 0
	}
	return 0
}

func ResolveReceiveTarget(chatID, actorUserID string) (string, string) {
	if strings.TrimSpace(chatID) != "" {
		return chatID, larkim.ReceiveIdTypeChatId
	}
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return "", ""
	}
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		return actorUserID, larkim.ReceiveIdTypeOpenId
	case strings.HasPrefix(actorUserID, "on_"):
		return actorUserID, larkim.ReceiveIdTypeUnionId
	default:
		return actorUserID, larkim.ReceiveIdTypeUserId
	}
}
