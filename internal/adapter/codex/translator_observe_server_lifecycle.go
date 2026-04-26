package codex

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func (t *Translator) observeThreadStarted(message map[string]any) Result {
	threadRecord := parseThreadRecord(lookupAny(message, "params", "thread"))
	t.applyPendingReviewThread(&threadRecord)
	threadID := threadRecord.ThreadID
	if threadID == "" {
		threadID = lookupString(message, "params", "threadId")
	}
	cwd := threadRecord.CWD
	if cwd == "" {
		cwd = lookupString(message, "params", "cwd")
	}
	name := threadRecord.Name
	runtimeStatus := agentproto.CloneThreadRuntimeStatus(threadRecord.RuntimeStatus)
	status := ""
	loaded := false
	if runtimeStatus != nil {
		status = runtimeStatus.LegacyState()
		loaded = runtimeStatus.IsLoaded()
	}
	if t.suppressedThreadStarted[threadID] {
		delete(t.suppressedThreadStarted, threadID)
		t.currentThreadID = threadID
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
		t.debugf("observe server suppressed thread/started after child restart: thread=%s cwd=%s", threadID, cwd)
		return Result{Suppress: true}
	}
	if t.internalThreadIDs[threadID] {
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
		event := buildThreadDiscoveredEvent(threadRecord, threadID, cwd, name, status, loaded, runtimeStatus)
		event.TrafficClass = agentproto.TrafficClassInternalHelper
		event.Initiator = agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
		event.Metadata = mergeEventMetadata(event.Metadata, map[string]any{"internalHelper": true})
		return Result{Events: []agentproto.Event{event}}
	}
	event := buildThreadDiscoveredEvent(threadRecord, threadID, cwd, name, status, loaded, runtimeStatus)
	t.currentThreadID = threadID
	if t.pendingLocalNewThreadTurn && threadID != "" {
		t.pendingLocalTurnByThread[threadID] = true
		t.pendingLocalNewThreadTurn = false
	}
	if cwd != "" {
		t.knownThreadCWD[threadID] = cwd
	}
	return Result{Events: []agentproto.Event{event}}
}

func buildThreadDiscoveredEvent(threadRecord agentproto.ThreadSnapshotRecord, threadID, cwd, name, status string, loaded bool, runtimeStatus *agentproto.ThreadRuntimeStatus) agentproto.Event {
	event := agentproto.Event{
		Kind:            agentproto.EventThreadDiscovered,
		ThreadID:        threadID,
		CWD:             cwd,
		Name:            name,
		Preview:         threadRecord.Preview,
		Model:           threadRecord.Model,
		ReasoningEffort: threadRecord.ReasoningEffort,
		PlanMode:        threadRecord.PlanMode,
		Status:          status,
		Loaded:          loaded,
		FocusSource:     "remote_created_thread",
		RuntimeStatus:   runtimeStatus,
	}
	if threadRecord.ForkedFromID != "" || threadRecord.Source != nil {
		event.Metadata = map[string]any{}
		if threadRecord.ForkedFromID != "" {
			event.Metadata["forkedFromId"] = threadRecord.ForkedFromID
		}
		if threadRecord.Source != nil {
			event.Metadata["threadSource"] = agentproto.CloneThreadSourceRecord(threadRecord.Source)
		}
	}
	return event
}

func mergeEventMetadata(left, right map[string]any) map[string]any {
	switch {
	case len(left) == 0 && len(right) == 0:
		return nil
	case len(left) == 0:
		return cloneMap(right)
	case len(right) == 0:
		return left
	default:
		merged := cloneMap(left)
		for key, value := range right {
			merged[key] = value
		}
		return merged
	}
}

func (t *Translator) observeThreadStatusChanged(message map[string]any) Result {
	threadID := lookupString(message, "params", "threadId")
	runtimeStatus := parseThreadRuntimeStatus(lookupAny(message, "params", "status"))
	if threadID == "" || runtimeStatus == nil {
		return Result{}
	}
	trafficClass := agentproto.TrafficClassPrimary
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface}
	if t.internalThreadIDs[threadID] {
		trafficClass = agentproto.TrafficClassInternalHelper
		initiator = agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
	}
	return Result{Events: []agentproto.Event{{
		Kind:          agentproto.EventThreadRuntimeStatusUpdated,
		ThreadID:      threadID,
		Status:        runtimeStatus.LegacyState(),
		Loaded:        runtimeStatus.IsLoaded(),
		TrafficClass:  trafficClass,
		Initiator:     initiator,
		RuntimeStatus: runtimeStatus,
	}}}
}

func (t *Translator) observeTurnStarted(message map[string]any) Result {
	threadID := lookupString(message, "params", "thread", "id")
	if threadID == "" {
		threadID = lookupString(message, "params", "threadId")
	}
	turnID := lookupString(message, "params", "turn", "id")
	if turnID == "" {
		turnID = lookupString(message, "params", "turnId")
	}
	trafficClass := t.trafficClassForTurn(threadID, turnID)
	pendingRemoteSurface := t.pendingRemoteTurnByThread[threadID]
	pendingLocal := t.pendingLocalTurnByThread[threadID]
	initiator := t.resolveTurnInitiator(threadID, turnID, trafficClass)
	if turnID != "" {
		t.turnInitiators[turnID] = initiator
	}
	t.debugf(
		"observe server turn/started: thread=%s turn=%s initiator=%s traffic=%s pendingRemoteSurface=%s pendingLocal=%t",
		threadID,
		turnID,
		initiator.Kind,
		trafficClass,
		pendingRemoteSurface,
		pendingLocal,
	)
	return Result{Events: []agentproto.Event{{
		Kind:         agentproto.EventTurnStarted,
		ThreadID:     threadID,
		TurnID:       turnID,
		Status:       "running",
		TrafficClass: trafficClass,
		Initiator:    initiator,
	}}}
}

func (t *Translator) observeTurnCompleted(message map[string]any) Result {
	threadID := lookupString(message, "params", "thread", "id")
	if threadID == "" {
		threadID = lookupString(message, "params", "threadId")
	}
	turnID := lookupString(message, "params", "turn", "id")
	if turnID == "" {
		turnID = lookupString(message, "params", "turnId")
	}
	trafficClass := t.trafficClassForTurn(threadID, turnID)
	status := lookupString(message, "params", "turn", "status")
	if status == "" {
		status = "completed"
	}
	errMsg := lookupString(message, "params", "turn", "error", "message")
	problem, hasProblem := t.pendingTurnProblems[turnID]
	delete(t.pendingTurnProblems, turnID)
	if status == "completed" {
		hasProblem = false
	}
	if errMsg == "" && hasProblem {
		errMsg = problem.Message
	}
	initiator := t.turnInitiators[turnID]
	if initiator.Kind == "" {
		initiator = t.resolveTurnInitiator(threadID, turnID, trafficClass)
	}
	delete(t.turnInitiators, turnID)
	delete(t.internalTurnIDs, turnID)
	t.debugf("observe server turn/completed: thread=%s turn=%s status=%s initiator=%s", threadID, turnID, status, initiator.Kind)
	event := agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		ThreadID:             threadID,
		TurnID:               turnID,
		Status:               status,
		ErrorMessage:         errMsg,
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
		TrafficClass:         trafficClass,
		Initiator:            initiator,
	}
	if hasProblem {
		problemCopy := problem
		if problemCopy.ThreadID == "" {
			problemCopy.ThreadID = threadID
		}
		if problemCopy.TurnID == "" {
			problemCopy.TurnID = turnID
		}
		event.Problem = &problemCopy
	}
	return Result{Events: []agentproto.Event{event}}
}
