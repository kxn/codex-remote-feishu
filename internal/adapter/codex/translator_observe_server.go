package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) ObserveServer(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}

	if id, ok := message["id"]; ok {
		requestID := fmt.Sprint(id)
		if pending, ok := t.pendingSuppressedResponse[requestID]; ok {
			delete(t.pendingSuppressedResponse, requestID)
			if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
				delete(t.pendingRemoteTurnByThread, pending.ThreadID)
				t.debugf("observe server suppressed response error: request=%s action=%s thread=%s error=%s", requestID, pending.Action, pending.ThreadID, errMsg)
				if pending.Action == "turn/start" {
					return Result{Events: []agentproto.Event{{
						Kind:                 agentproto.EventTurnCompleted,
						ThreadID:             pending.ThreadID,
						Status:               "failed",
						ErrorMessage:         errMsg,
						TurnCompletionOrigin: agentproto.TurnCompletionOriginTurnStartRejected,
					}}}, nil
				}
				if pending.Action == "thread/compact/start" {
					return Result{Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
						Code:             "compact_start_failed",
						Layer:            "server",
						Stage:            "command_response",
						Operation:        "thread.compact.start",
						Message:          "Codex 拒绝了这次上下文整理请求。",
						Details:          errMsg,
						SurfaceSessionID: pending.SurfaceSessionID,
						ThreadID:         pending.ThreadID,
					})}}, nil
				}
				return Result{}, nil
			}
			t.debugf("observe server suppressed response: request=%s", requestID)
			return Result{Suppress: true}, nil
		}
		if t.pendingInternalTurnSet[requestID] {
			delete(t.pendingInternalTurnSet, requestID)
			turnID := lookupString(message, "result", "turn", "id")
			if turnID == "" {
				turnID = lookupString(message, "result", "id")
			}
			if turnID != "" {
				t.internalTurnIDs[turnID] = true
				t.turnInitiators[turnID] = agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
			}
			return Result{}, nil
		}
		if pending, exists := t.pendingChildRestartRestore[requestID]; exists {
			delete(t.pendingChildRestartRestore, requestID)
			if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
				t.debugf("observe server child restart restore error: request=%s thread=%s error=%s", requestID, pending.ThreadID, errMsg)
				return Result{Suppress: true}, nil
			}
			t.currentThreadID = pending.ThreadID
			if pending.CWD != "" {
				t.knownThreadCWD[pending.ThreadID] = pending.CWD
			}
			t.suppressedThreadStarted[pending.ThreadID] = true
			t.debugf("observe server child restart restore result: request=%s thread=%s", requestID, pending.ThreadID)
			return Result{Suppress: true}, nil
		}
		if pending, exists := t.pendingReviewStart[requestID]; exists {
			delete(t.pendingReviewStart, requestID)
			if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
				t.debugf("observe server review/start error: request=%s thread=%s error=%s", requestID, pending.ThreadID, errMsg)
				return Result{
					Suppress: true,
					Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
						Code:             "review_start_failed",
						Layer:            "server",
						Stage:            "command_response",
						Operation:        "review.start",
						Message:          "Codex 拒绝了这次审阅启动请求。",
						Details:          errMsg,
						SurfaceSessionID: pending.Initiator.SurfaceSessionID,
						ThreadID:         pending.ThreadID,
						RequestID:        requestID,
					})},
				}, nil
			}
			turnID := lookupString(message, "result", "turn", "id")
			reviewThreadID := lookupString(message, "result", "reviewThreadId")
			if reviewThreadID == "" {
				reviewThreadID = pending.ThreadID
			}
			if turnID != "" {
				t.turnInitiators[turnID] = pending.Initiator
			}
			if strings.TrimSpace(reviewThreadID) != "" {
				t.pendingReviewThreads[reviewThreadID] = pendingReviewThread{
					ParentThreadID: pending.ThreadID,
					Initiator:      pending.Initiator,
				}
			}
			t.debugf("observe server review/start result: request=%s parentThread=%s reviewThread=%s turn=%s initiator=%s", requestID, pending.ThreadID, reviewThreadID, turnID, pending.Initiator.Kind)
			return Result{Suppress: true}, nil
		}
		if pending, exists := t.pendingThreadCreate[requestID]; exists {
			delete(t.pendingThreadCreate, requestID)
			if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
				delete(t.pendingInternalThreadSet, requestID)
				action := choose(strings.TrimSpace(pending.Action), "thread/start")
				t.debugf("observe server %s error: request=%s error=%s", action, requestID, errMsg)
				return Result{Events: []agentproto.Event{{
					Kind:                 agentproto.EventTurnCompleted,
					Status:               "failed",
					ErrorMessage:         errMsg,
					TurnCompletionOrigin: agentproto.TurnCompletionOriginThreadStartRejected,
				}}}, nil
			}
			threadID := lookupString(message, "result", "thread", "id")
			if threadID == "" {
				threadID = lookupString(message, "result", "id")
			}
			if t.pendingInternalThreadSet[requestID] {
				delete(t.pendingInternalThreadSet, requestID)
				if threadID != "" {
					t.internalThreadIDs[threadID] = true
				}
			}
			t.currentThreadID = threadID
			if pending.Command.Target.CWD != "" {
				t.knownThreadCWD[threadID] = pending.Command.Target.CWD
			}
			followup, followupID, err := t.directTurnStart(threadID, pending.Command, true)
			if err != nil {
				return Result{}, err
			}
			action := choose(strings.TrimSpace(pending.Action), "thread/start")
			t.debugf("observe server %s result: request=%s thread=%s followup=%s", action, requestID, threadID, followupID)
			return Result{
				Suppress:        true,
				OutboundToCodex: [][]byte{followup},
			}, nil
		}
		if t.pendingInternalThreadSet[requestID] {
			delete(t.pendingInternalThreadSet, requestID)
			threadID := lookupString(message, "result", "thread", "id")
			if threadID == "" {
				threadID = lookupString(message, "result", "id")
			}
			if threadID != "" {
				t.internalThreadIDs[threadID] = true
			}
			return Result{}, nil
		}
		if pending, exists := t.pendingThreadResume[requestID]; exists {
			delete(t.pendingThreadResume, requestID)
			if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
				t.debugf("observe server thread/resume error: request=%s thread=%s kind=%s error=%s", requestID, pending.ThreadID, pending.Command.Kind, errMsg)
				if pending.Command.Kind == agentproto.CommandThreadCompactStart {
					return Result{Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
						Code:             "compact_start_failed",
						Layer:            "server",
						Stage:            "thread_resume_response",
						Operation:        "thread.compact.start",
						Message:          "Codex 拒绝了这次上下文整理请求。",
						Details:          errMsg,
						SurfaceSessionID: choose(pending.Command.Origin.Surface, pending.Command.Origin.ChatID),
						ThreadID:         pending.ThreadID,
					})}}, nil
				}
				return Result{Events: []agentproto.Event{{
					Kind:                 agentproto.EventTurnCompleted,
					ThreadID:             pending.ThreadID,
					Status:               "failed",
					ErrorMessage:         errMsg,
					TurnCompletionOrigin: agentproto.TurnCompletionOriginThreadResumeRejected,
				}}}, nil
			}
			t.currentThreadID = pending.ThreadID
			if pending.Command.Target.CWD != "" {
				t.knownThreadCWD[pending.ThreadID] = pending.Command.Target.CWD
			}
			switch pending.Command.Kind {
			case agentproto.CommandThreadCompactStart:
				followup, followupID, err := t.directCompactStart(pending.Command)
				if err != nil {
					return Result{}, err
				}
				t.debugf("observe server thread/resume result: request=%s thread=%s compactFollowup=%s", requestID, pending.ThreadID, followupID)
				return Result{
					Suppress:        true,
					OutboundToCodex: [][]byte{followup},
				}, nil
			default:
				followup, followupID, err := t.directTurnStart(pending.ThreadID, pending.Command, false)
				if err != nil {
					return Result{}, err
				}
				t.debugf("observe server thread/resume result: request=%s thread=%s followup=%s", requestID, pending.ThreadID, followupID)
				return Result{
					Suppress:        true,
					OutboundToCodex: [][]byte{followup},
				}, nil
			}
		}
		if pending, exists := t.pendingThreadNameSet[requestID]; exists {
			delete(t.pendingThreadNameSet, requestID)
			if _, hasError := message["error"]; hasError {
				return Result{}, nil
			}
			name := choose(
				pending.Name,
				lookupString(message, "result", "thread", "name"),
				lookupString(message, "result", "name"),
			)
			if pending.ThreadID == "" || name == "" {
				return Result{}, nil
			}
			return Result{
				Events: []agentproto.Event{{
					Kind:     agentproto.EventThreadDiscovered,
					ThreadID: pending.ThreadID,
					Name:     name,
				}},
			}, nil
		}
		var threadListAliasResponses [][]byte
		if resolution, ok, err := t.threadListBroker.ResolveResponse(message); err != nil {
			return Result{}, err
		} else if ok {
			threadListAliasResponses = resolution.AliasResponses
		}
		if t.threadListRefreshOwnsResponse(requestID) {
			refresh := t.threadListRefresh
			delete(refresh.records, "")
			borrowed := refresh.ownerVisible
			refresh.ownerRequestID = ""
			refresh.ownerVisible = false
			refresh.order = nil
			threads := parseThreadList(message["result"])
			t.debugf(
				"observe server thread/list refresh: request=%s borrowed=%t threads=%d currentThread=%s",
				requestID,
				borrowed,
				len(threads),
				t.currentThreadID,
			)
			if len(threads) == 0 {
				refresh.records = map[string]agentproto.ThreadSnapshotRecord{}
				return Result{
					Suppress:         !borrowed,
					OutboundToParent: threadListAliasResponses,
					Events: []agentproto.Event{{
						Kind:    agentproto.EventThreadsSnapshot,
						Threads: nil,
					}},
				}, nil
			}
			var outbound [][]byte
			for index, thread := range threads {
				thread.ListOrder = index + 1
				refresh.records[thread.ThreadID] = thread
				refresh.order = append(refresh.order, thread.ThreadID)
				if !threadRefreshNeedsRead(thread) {
					continue
				}
				readID := t.nextRequest("thread-read")
				refresh.pendingReads[readID] = thread.ThreadID
				payload := map[string]any{
					"id":     readID,
					"method": "thread/read",
					"params": map[string]any{
						"threadId": thread.ThreadID,
					},
				}
				bytes, err := json.Marshal(payload)
				if err != nil {
					return Result{}, err
				}
				outbound = append(outbound, append(bytes, '\n'))
			}
			if len(outbound) == 0 {
				t.debugf(
					"observe server thread/list refresh satisfied from list: request=%s borrowed=%t threads=%d",
					requestID,
					borrowed,
					len(threads),
				)
				result := t.finishThreadListRefresh(!borrowed)
				result.OutboundToParent = threadListAliasResponses
				return result, nil
			}
			t.debugf(
				"observe server thread/list refresh followups: request=%s borrowed=%t threadReads=%d firstThread=%s",
				requestID,
				borrowed,
				len(outbound),
				threads[0].ThreadID,
			)
			return Result{
				Suppress:         !borrowed,
				OutboundToCodex:  outbound,
				OutboundToParent: threadListAliasResponses,
			}, nil
		}
		if len(threadListAliasResponses) != 0 {
			return Result{OutboundToParent: threadListAliasResponses}, nil
		}
		if t.threadListRefresh != nil {
			if threadID, exists := t.threadListRefresh.pendingReads[requestID]; exists {
				record := t.threadListRefresh.records[threadID]
				patch := parseThreadRecord(message["result"])
				record.ThreadID = choose(patch.ThreadID, record.ThreadID)
				record.ForkedFromID = choose(patch.ForkedFromID, record.ForkedFromID)
				if patch.Source != nil {
					record.Source = agentproto.CloneThreadSourceRecord(patch.Source)
				}
				record.Name = choose(patch.Name, record.Name)
				record.Preview = choose(patch.Preview, record.Preview)
				record.CWD = choose(patch.CWD, record.CWD)
				record.PlanMode = choose(patch.PlanMode, record.PlanMode)
				record.Loaded = record.Loaded || patch.Loaded
				record.Archived = record.Archived || patch.Archived
				record.State = choose(patch.State, record.State)
				if patch.RuntimeStatus != nil {
					record.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(patch.RuntimeStatus)
					record.Loaded = patch.RuntimeStatus.IsLoaded()
					record.State = choose(patch.RuntimeStatus.LegacyState(), record.State)
				}
				t.threadListRefresh.records[threadID] = record
				delete(t.threadListRefresh.pendingReads, requestID)
				if len(t.threadListRefresh.pendingReads) == 0 {
					result := t.finishThreadListRefresh(true)
					t.debugf(
						"observe server thread refresh completed: request=%s records=%d currentThread=%s",
						requestID,
						len(result.Events[0].Threads),
						t.currentThreadID,
					)
					return result, nil
				}
				return Result{Suppress: true}, nil
			}
		}
		if pending, exists := t.pendingThreadHistoryReads[requestID]; exists {
			delete(t.pendingThreadHistoryReads, requestID)
			history := parseThreadHistoryRecord(message["result"])
			threadID := choose(history.Thread.ThreadID, pending.ThreadID)
			if threadID == "" {
				threadID = pending.ThreadID
			}
			history.Thread.ThreadID = threadID
			return Result{
				Suppress: true,
				Events: []agentproto.Event{{
					Kind:          agentproto.EventThreadHistoryRead,
					CommandID:     pending.CommandID,
					ThreadID:      threadID,
					ThreadHistory: &history,
				}},
			}, nil
		}
	}

	method, _ := message["method"].(string)
	switch method {
	case "error":
		problem := parseCodexProblemEvent(message)
		if problem == nil {
			return Result{}, nil
		}
		if problem.TurnID != "" {
			t.pendingTurnProblems[problem.TurnID] = *problem
			if t.threadListRefreshActive() {
				t.debugf(
					"observe server error during thread refresh: thread=%s turn=%s code=%s pendingThreadList=%t pendingThreadReads=%d currentThread=%s",
					problem.ThreadID,
					problem.TurnID,
					problem.Code,
					t.threadListRefreshWaitingForList(),
					t.threadListRefreshPendingReadCount(),
					t.currentThreadID,
				)
			}
			t.debugf(
				"observe server error: thread=%s turn=%s code=%s retryable=%t message=%s",
				problem.ThreadID,
				problem.TurnID,
				problem.Code,
				problem.Retryable,
				problem.Message,
			)
			// Turn-bound runtime errors are attached to the terminal turn event so
			// Feishu receives one precise failure card instead of duplicate alerts.
			return Result{}, nil
		}
		t.debugf("observe server error without turn: code=%s message=%s", problem.Code, problem.Message)
		return Result{Events: []agentproto.Event{agentproto.NewSystemErrorEvent(*problem)}}, nil
	case "thread/started":
		return t.observeThreadStarted(message), nil
	case "thread/status/changed":
		return t.observeThreadStatusChanged(message), nil
	case "thread/name/updated":
		threadID := lookupString(message, "params", "threadId")
		threadRecord := parseThreadRecord(lookupAny(message, "params", "thread"))
		if t.internalThreadIDs[threadID] {
			name := lookupString(message, "params", "name")
			if name == "" {
				name = lookupString(message, "params", "thread", "name")
			}
			return Result{Events: []agentproto.Event{{
				Kind:         agentproto.EventThreadDiscovered,
				ThreadID:     threadID,
				Name:         name,
				PlanMode:     threadRecord.PlanMode,
				TrafficClass: agentproto.TrafficClassInternalHelper,
				Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
				Metadata:     map[string]any{"internalHelper": true},
			}}}, nil
		}
		name := lookupString(message, "params", "name")
		if name == "" {
			name = lookupString(message, "params", "thread", "name")
		}
		return Result{Events: []agentproto.Event{{
			Kind:     agentproto.EventThreadDiscovered,
			ThreadID: threadID,
			Name:     name,
			PlanMode: threadRecord.PlanMode,
		}}}, nil
	case "thread/tokenUsage/updated":
		threadID, turnID, usage := extractThreadTokenUsageNotification(message)
		if threadID == "" || usage == nil {
			return Result{}, nil
		}
		return Result{Events: []agentproto.Event{{
			Kind:       agentproto.EventThreadTokenUsageUpdated,
			ThreadID:   threadID,
			TurnID:     turnID,
			TokenUsage: usage,
		}}}, nil
	case "turn/plan/updated":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		snapshot := extractTurnPlanSnapshot(message)
		if snapshot == nil {
			return Result{}, nil
		}
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventTurnPlanUpdated,
			ThreadID:     threadID,
			TurnID:       turnID,
			PlanSnapshot: snapshot,
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	case "turn/diff/updated":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		diff := lookupString(message, "params", "diff")
		if threadID == "" || turnID == "" {
			return Result{}, nil
		}
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventTurnDiffUpdated,
			ThreadID:     threadID,
			TurnID:       turnID,
			TurnDiff:     diff,
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	case "model/rerouted":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		reroute := agentproto.NormalizeTurnModelReroute(&agentproto.TurnModelReroute{
			ThreadID:  threadID,
			TurnID:    turnID,
			FromModel: lookupString(message, "params", "fromModel"),
			ToModel:   lookupString(message, "params", "toModel"),
			Reason:    lookupString(message, "params", "reason"),
		})
		if reroute == nil || reroute.ThreadID == "" || reroute.TurnID == "" {
			return Result{}, nil
		}
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventTurnModelRerouted,
			ThreadID:     reroute.ThreadID,
			TurnID:       reroute.TurnID,
			ModelReroute: reroute,
			TrafficClass: t.trafficClassForTurn(reroute.ThreadID, reroute.TurnID),
			Initiator:    t.initiatorForTurn(reroute.ThreadID, reroute.TurnID),
		}}}, nil
	case "turn/started":
		return t.observeTurnStarted(message), nil
	case "turn/completed":
		return t.observeTurnCompleted(message), nil
	case "serverRequest/started", "request/started":
		request := extractRequestPayload(message)
		requestID := extractRequestID(message, request)
		if requestID == "" {
			return Result{}, nil
		}
		threadID := extractRequestThreadID(message, request)
		turnID := extractRequestTurnID(message, request)
		prompt := extractRequestPrompt(method, message)
		if prompt != nil {
			t.pendingRequestTypes[requestID] = prompt.Type
		}
		return Result{Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			ThreadID:      threadID,
			TurnID:        turnID,
			RequestID:     requestID,
			Status:        "pending",
			TrafficClass:  t.trafficClassForTurn(threadID, turnID),
			Initiator:     t.initiatorForTurn(threadID, turnID),
			RequestPrompt: prompt,
			Metadata:      extractRequestMetadata(method, message, prompt),
		}}}, nil
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "mcpServer/elicitation/request":
		requestID := extractRequestID(message, nil)
		if requestID == "" {
			return Result{}, nil
		}
		threadID := extractRequestThreadID(message, nil)
		turnID := extractRequestTurnID(message, nil)
		prompt := extractRequestPrompt(method, message)
		if prompt != nil {
			t.pendingRequestTypes[requestID] = prompt.Type
		}
		return Result{Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			ThreadID:      threadID,
			TurnID:        turnID,
			RequestID:     requestID,
			Status:        "pending",
			TrafficClass:  t.trafficClassForTurn(threadID, turnID),
			Initiator:     t.initiatorForTurn(threadID, turnID),
			RequestPrompt: prompt,
			Metadata:      extractRequestMetadata(method, message, prompt),
		}}}, nil
	case "tool/requestUserInput", "item/tool/requestUserInput", "item/tool/call":
		requestID := extractRequestID(message, nil)
		if requestID == "" {
			return Result{}, nil
		}
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		prompt := extractRequestPrompt(method, message)
		if prompt != nil {
			t.pendingRequestTypes[requestID] = prompt.Type
		}
		return Result{Events: []agentproto.Event{{
			Kind:          agentproto.EventRequestStarted,
			ThreadID:      threadID,
			TurnID:        turnID,
			RequestID:     requestID,
			Status:        "pending",
			TrafficClass:  t.trafficClassForTurn(threadID, turnID),
			Initiator:     t.initiatorForTurn(threadID, turnID),
			RequestPrompt: prompt,
			Metadata:      extractRequestMetadata(method, message, prompt),
		}}}, nil
	case "item/mcpToolCall/progress":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		itemID := lookupString(message, "params", "itemId")
		progressMessage := firstNonEmptyString(
			lookupString(message, "params", "message"),
			lookupString(message, "params", "progress", "message"),
		)
		if itemID == "" || progressMessage == "" {
			return Result{}, nil
		}
		return Result{Events: []agentproto.Event{{
			Kind:            agentproto.EventItemDelta,
			ThreadID:        threadID,
			TurnID:          turnID,
			ItemID:          itemID,
			ItemKind:        "mcp_tool_call",
			Status:          "inProgress",
			Delta:           progressMessage,
			TrafficClass:    t.trafficClassForTurn(threadID, turnID),
			Initiator:       t.initiatorForTurn(threadID, turnID),
			MCPToolProgress: &agentproto.MCPToolCallProgress{Message: progressMessage},
			Metadata: map[string]any{
				"progressMessage": progressMessage,
			},
		}}}, nil
	case "item/autoApprovalReview/started", "item/autoApprovalReview/completed":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		targetItemID := lookupString(message, "params", "targetItemId")
		action := cloneMap(lookupMap(message, "params", "action"))
		review := cloneMap(lookupMap(message, "params", "review"))
		metadata := map[string]any{}
		if targetItemID != "" {
			metadata["targetItemId"] = targetItemID
		}
		if len(action) != 0 {
			metadata["action"] = action
		}
		if len(review) != 0 {
			metadata["review"] = review
		}
		actionType := firstNonEmptyString(
			lookupStringFromAny(action["type"]),
			lookupString(message, "params", "action", "type"),
		)
		if actionType != "" {
			metadata["actionType"] = actionType
		}
		kind := agentproto.EventItemStarted
		status := "started"
		if method == "item/autoApprovalReview/completed" {
			kind = agentproto.EventItemCompleted
			status = "completed"
		}
		return Result{Events: []agentproto.Event{{
			Kind:         kind,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       targetItemID,
			ItemKind:     "auto_approval_review",
			Status:       status,
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			ApprovalReview: &agentproto.AutoApprovalReview{
				TargetItemID: targetItemID,
				ActionType:   actionType,
				Action:       action,
				Review:       review,
			},
			Metadata: metadata,
		}}}, nil
	case "serverRequest/resolved", "request/resolved":
		params := lookupMap(message, "params")
		request := extractRequestPayload(message)
		requestID := extractRequestID(message, request)
		if requestID == "" {
			return Result{}, nil
		}
		threadID := extractRequestThreadID(message, request)
		turnID := extractRequestTurnID(message, request)
		requestType := extractRequestType(method, request, params)
		if requestType == "" {
			requestType = string(t.pendingRequestTypes[requestID])
		}
		delete(t.pendingRequestTypes, requestID)
		metadata := extractResolvedRequestMetadata(requestType, request, params)
		status := firstNonEmptyString(
			lookupStringFromAny(params["status"]),
			lookupStringFromAny(request["status"]),
			"resolved",
		)
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventRequestResolved,
			ThreadID:     threadID,
			TurnID:       turnID,
			RequestID:    requestID,
			Status:       status,
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     metadata,
		}}}, nil
	case "item/completed":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		item := lookupMap(message, "params", "item")
		itemID := choose(
			lookupStringFromAny(item["id"]),
			lookupString(message, "params", "itemId"),
		)
		itemKind := normalizeItemKind(choose(
			lookupStringFromAny(item["type"]),
			lookupString(message, "params", "itemType"),
		))
		metadata := extractItemMetadata(itemKind, item)
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemCompleted,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			ItemKind:     itemKind,
			Status:       extractItemStatus(item),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     metadata,
			FileChanges:  extractFileChangeRecords(itemKind, item),
		}}}, nil
	case "item/started":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		item := lookupMap(message, "params", "item")
		itemID := choose(
			lookupStringFromAny(item["id"]),
			lookupString(message, "params", "itemId"),
		)
		itemKind := normalizeItemKind(choose(
			lookupStringFromAny(item["type"]),
			lookupString(message, "params", "itemType"),
		))
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemStarted,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			ItemKind:     itemKind,
			Status:       extractItemStatus(item),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     extractItemMetadata(itemKind, item),
			FileChanges:  extractFileChangeRecords(itemKind, item),
		}}}, nil
	case "item/agentMessage/delta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "agent_message",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	case "item/plan/delta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "plan",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	case "item/reasoning/summaryTextDelta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "reasoning_summary",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     map[string]any{"summaryIndex": lookupIntFromAny(lookupAny(message, "params", "summaryIndex"))},
		}}}, nil
	case "item/reasoning/textDelta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "reasoning_content",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     map[string]any{"contentIndex": lookupIntFromAny(lookupAny(message, "params", "contentIndex"))},
		}}}, nil
	case "item/commandExecution/outputDelta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "command_execution_output",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	case "item/fileChange/outputDelta":
		threadID := lookupString(message, "params", "threadId")
		turnID := lookupString(message, "params", "turnId")
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       lookupString(message, "params", "itemId"),
			ItemKind:     "file_change_output",
			Delta:        lookupString(message, "params", "delta"),
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
		}}}, nil
	default:
		return Result{}, nil
	}
}

func threadRefreshNeedsRead(record agentproto.ThreadSnapshotRecord) bool {
	if strings.TrimSpace(record.CWD) == "" {
		return true
	}
	if strings.TrimSpace(record.Name) == "" && strings.TrimSpace(record.Preview) == "" {
		return true
	}
	if record.RuntimeStatus == nil && strings.TrimSpace(record.State) == "" {
		return true
	}
	return false
}

func parseCodexProblemEvent(message map[string]any) *agentproto.ErrorInfo {
	errPayload := lookupMap(message, "params", "error")
	if len(errPayload) == 0 {
		return nil
	}
	messageText := strings.TrimSpace(lookupStringFromAny(errPayload["message"]))
	detailsText := strings.TrimSpace(lookupStringFromAny(errPayload["additionalDetails"]))
	retryable := lookupBool(message, "params", "willRetry")
	if retryable && detailsText != "" && strings.HasPrefix(strings.ToLower(messageText), "reconnecting") {
		messageText = detailsText
	}
	problem := agentproto.ErrorInfo{
		Code:      firstNonEmptyString(codexErrorCode(errPayload["codexErrorInfo"]), "codex_runtime_error"),
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   firstNonEmptyString(messageText, detailsText),
		Details:   firstNonEmptyString(detailsText, messageText),
		ThreadID:  lookupString(message, "params", "threadId"),
		TurnID:    lookupString(message, "params", "turnId"),
		Retryable: retryable,
	}
	normalized := problem.Normalize()
	return &normalized
}

func codexErrorCode(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if len(typed) != 1 {
			return ""
		}
		for key := range typed {
			return strings.TrimSpace(key)
		}
	}
	return ""
}

func extractTurnPlanSnapshot(message map[string]any) *agentproto.TurnPlanSnapshot {
	params := lookupMap(message, "params")
	if len(params) == 0 {
		return nil
	}
	snapshot := &agentproto.TurnPlanSnapshot{
		Explanation: strings.TrimSpace(lookupStringFromAny(params["explanation"])),
	}
	rawPlan, _ := params["plan"].([]any)
	for _, raw := range rawPlan {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		step := strings.TrimSpace(lookupStringFromAny(entry["step"]))
		status := agentproto.NormalizeTurnPlanStepStatus(lookupStringFromAny(entry["status"]))
		if step == "" && status == "" {
			continue
		}
		snapshot.Steps = append(snapshot.Steps, agentproto.TurnPlanStep{
			Step:   step,
			Status: status,
		})
	}
	if snapshot.Explanation == "" && len(snapshot.Steps) == 0 {
		return nil
	}
	return snapshot
}

func lookupBool(message map[string]any, path ...string) bool {
	current := any(message)
	for _, segment := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, exists := m[segment]
		if !exists {
			return false
		}
		current = next
	}
	value, ok := current.(bool)
	return ok && value
}
