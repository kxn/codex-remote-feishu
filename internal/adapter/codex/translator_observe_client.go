package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (t *Translator) ObserveClient(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}
	method, _ := message["method"].(string)
	params, _ := message["params"].(map[string]any)

	switch method {
	case "thread/list":
		if requestID, ok := message["id"]; ok {
			return t.observeClientThreadList(fmt.Sprint(requestID), params), nil
		}
		return Result{}, nil
	case "review/start":
		threadID, _ := params["threadId"].(string)
		if requestID, ok := message["id"]; ok {
			t.pendingReviewStart[fmt.Sprint(requestID)] = pendingReviewStart{
				ThreadID:  strings.TrimSpace(threadID),
				Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
			}
		}
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventLocalInteractionObserved,
			ThreadID:     strings.TrimSpace(threadID),
			Action:       "review_start",
			TrafficClass: agentproto.TrafficClassPrimary,
			Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
		}}}, nil
	case "thread/resume":
		threadID, _ := params["threadId"].(string)
		cwd, _ := params["cwd"].(string)
		t.currentThreadID = threadID
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
		t.debugf("observe client thread/resume: thread=%s cwd=%s", threadID, cwd)
		return Result{Events: []agentproto.Event{{
			Kind:        agentproto.EventThreadFocused,
			ThreadID:    threadID,
			CWD:         cwd,
			FocusSource: "local_ui",
		}}}, nil
	case "thread/start":
		if isInternalLocalThreadStart(params) {
			if requestID, ok := message["id"]; ok {
				t.pendingInternalThreadSet[fmt.Sprint(requestID)] = true
			}
			return Result{}, nil
		}
		t.latestThreadStartParams = normalizeThreadStartParams(params)
		return Result{Events: configObservedEvents("", lookupStringFromAny(params["cwd"]), params, true)}, nil
	case "turn/start":
		threadID, _ := params["threadId"].(string)
		cwd, _ := params["cwd"].(string)
		if isInternalLocalTurnStart(params) {
			if requestID, ok := message["id"]; ok {
				t.pendingInternalTurnSet[fmt.Sprint(requestID)] = true
			}
			t.debugf("observe client turn/start internal-helper: thread=%s cwd=%s", threadID, cwd)
			return Result{Events: []agentproto.Event{{
				Kind:         agentproto.EventLocalInteractionObserved,
				ThreadID:     threadID,
				CWD:          cwd,
				Action:       "turn_start",
				TrafficClass: agentproto.TrafficClassInternalHelper,
				Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
			}}}, nil
		}
		t.currentThreadID = threadID
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
		template := normalizeTurnStartTemplate(params)
		t.latestTurnStartTemplate = template
		if threadID != "" {
			t.turnStartByThread[threadID] = template
			t.pendingLocalTurnByThread[threadID] = true
		} else {
			t.pendingLocalNewThreadTurn = true
		}
		if !isNull(template["approvalPolicy"]) || !isNull(template["sandboxPolicy"]) {
			t.newThreadTurnTemplate = cloneMap(template)
		}
		t.debugf("observe client turn/start: thread=%s cwd=%s newThread=%t", threadID, cwd, threadID == "")
		events := configObservedEvents(threadID, cwd, params, threadID == "")
		if threadID != "" && !configObservedEventsContainThreadPlan(events, threadID) {
			events = append(events, agentproto.Event{
				Kind:         agentproto.EventConfigObserved,
				ThreadID:     threadID,
				CWD:          cwd,
				PlanMode:     "off",
				ConfigScope:  "thread",
				TrafficClass: agentproto.TrafficClassPrimary,
				Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
			})
		}
		events = append(events, agentproto.Event{
			Kind:         agentproto.EventLocalInteractionObserved,
			ThreadID:     threadID,
			CWD:          cwd,
			Action:       "turn_start",
			TrafficClass: agentproto.TrafficClassPrimary,
			Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
		})
		return Result{Events: events}, nil
	case "turn/steer":
		threadID, _ := params["threadId"].(string)
		if threadID == "" {
			threadID = t.currentThreadID
		}
		if threadID != "" {
			t.pendingLocalTurnByThread[threadID] = true
		}
		t.debugf("observe client turn/steer: thread=%s", threadID)
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventLocalInteractionObserved,
			ThreadID:     threadID,
			Action:       "turn_steer",
			TrafficClass: agentproto.TrafficClassPrimary,
			Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
		}}}, nil
	case "thread/name/set":
		if requestID, ok := message["id"]; ok {
			t.pendingThreadNameSet[fmt.Sprint(requestID)] = pendingThreadNameSet{
				ThreadID: lookupStringFromAny(params["threadId"]),
				Name:     lookupStringFromAny(params["name"]),
			}
		}
		return Result{}, nil
	default:
		return Result{}, nil
	}
}

func configObservedEventsContainThreadPlan(events []agentproto.Event, threadID string) bool {
	for _, event := range events {
		if event.Kind != agentproto.EventConfigObserved {
			continue
		}
		if event.ConfigScope != "thread" {
			continue
		}
		if event.ThreadID != threadID {
			continue
		}
		if event.PlanMode != "" {
			return true
		}
	}
	return false
}

func (t *Translator) observeClientThreadList(requestID string, params map[string]any) Result {
	observation := t.threadListBroker.ObserveClientRequest(requestID, params)
	if observation.OwnerRequestID == "" {
		return Result{}
	}
	if observation.Suppress {
		t.debugf(
			"observe client thread/list joined inflight owner=%s alias=%s key=%s visible=%t aliases=%d",
			observation.OwnerRequestID,
			requestID,
			observation.Key,
			observation.OwnerVisible,
			observation.AliasCount,
		)
		return Result{Suppress: true}
	}
	if observation.NewOwner {
		t.debugf("observe client thread/list owner=%s key=%s", requestID, observation.Key)
	}
	return Result{}
}
