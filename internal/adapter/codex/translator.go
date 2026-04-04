package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type Translator struct {
	instanceID                string
	nextID                    int
	currentThreadID           string
	knownThreadCWD            map[string]string
	pendingRemoteTurnByThread map[string]string
	pendingLocalTurnByThread  map[string]bool
	pendingLocalNewThreadTurn bool
	pendingThreadCreate       map[string]pendingThreadCreate
	pendingThreadResume       map[string]pendingThreadResume
	pendingThreadNameSet      map[string]pendingThreadNameSet
	pendingInternalThreadSet  map[string]bool
	pendingInternalTurnSet    map[string]bool
	internalThreadIDs         map[string]bool
	internalTurnIDs           map[string]bool
	turnInitiators            map[string]agentproto.Initiator

	latestThreadStartParams map[string]any
	latestTurnStartTemplate map[string]any
	turnStartByThread       map[string]map[string]any
	newThreadTurnTemplate   map[string]any

	pendingThreadListRequestID string
	pendingThreadReads         map[string]string
	threadRefreshRecords       map[string]agentproto.ThreadSnapshotRecord
	threadRefreshOrder         []string
	pendingSuppressedResponse  map[string]bool
}

type pendingThreadCreate struct {
	Command agentproto.Command
}

type pendingThreadResume struct {
	ThreadID string
	Command  agentproto.Command
}

type pendingThreadNameSet struct {
	ThreadID string
	Name     string
}

type Result struct {
	Events          []agentproto.Event
	OutboundToCodex [][]byte
	Suppress        bool
}

func NewTranslator(instanceID string) *Translator {
	return &Translator{
		instanceID:                instanceID,
		knownThreadCWD:            map[string]string{},
		pendingRemoteTurnByThread: map[string]string{},
		pendingLocalTurnByThread:  map[string]bool{},
		pendingThreadCreate:       map[string]pendingThreadCreate{},
		pendingThreadResume:       map[string]pendingThreadResume{},
		pendingThreadNameSet:      map[string]pendingThreadNameSet{},
		pendingInternalThreadSet:  map[string]bool{},
		pendingInternalTurnSet:    map[string]bool{},
		internalThreadIDs:         map[string]bool{},
		internalTurnIDs:           map[string]bool{},
		turnInitiators:            map[string]agentproto.Initiator{},
		turnStartByThread:         map[string]map[string]any{},
		pendingThreadReads:        map[string]string{},
		threadRefreshRecords:      map[string]agentproto.ThreadSnapshotRecord{},
		pendingSuppressedResponse: map[string]bool{},
	}
}

func (t *Translator) ObserveClient(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}
	method, _ := message["method"].(string)
	params, _ := message["params"].(map[string]any)

	switch method {
	case "thread/resume":
		threadID, _ := params["threadId"].(string)
		cwd, _ := params["cwd"].(string)
		t.currentThreadID = threadID
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
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
		events := configObservedEvents(threadID, cwd, params, threadID == "")
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

func (t *Translator) ObserveServer(raw []byte) (Result, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return Result{}, err
	}

	if id, ok := message["id"]; ok {
		requestID := fmt.Sprint(id)
		if t.pendingSuppressedResponse[requestID] {
			delete(t.pendingSuppressedResponse, requestID)
			return Result{Suppress: true}, nil
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
		if pending, exists := t.pendingThreadCreate[requestID]; exists {
			threadID := lookupString(message, "result", "thread", "id")
			if threadID == "" {
				threadID = lookupString(message, "result", "id")
			}
			delete(t.pendingThreadCreate, requestID)
			t.currentThreadID = threadID
			if pending.Command.Target.CWD != "" {
				t.knownThreadCWD[threadID] = pending.Command.Target.CWD
			}
			followup, err := t.directTurnStart(threadID, pending.Command, true)
			if err != nil {
				return Result{}, err
			}
			return Result{
				Suppress:        true,
				OutboundToCodex: [][]byte{followup},
			}, nil
		}
		if pending, exists := t.pendingThreadResume[requestID]; exists {
			delete(t.pendingThreadResume, requestID)
			t.currentThreadID = pending.ThreadID
			if pending.Command.Target.CWD != "" {
				t.knownThreadCWD[pending.ThreadID] = pending.Command.Target.CWD
			}
			followup, err := t.directTurnStart(pending.ThreadID, pending.Command, false)
			if err != nil {
				return Result{}, err
			}
			return Result{
				Suppress:        true,
				OutboundToCodex: [][]byte{followup},
			}, nil
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
		if requestID == t.pendingThreadListRequestID {
			delete(t.threadRefreshRecords, "")
			t.pendingThreadListRequestID = ""
			t.threadRefreshOrder = nil
			threads := parseThreadList(message["result"])
			if len(threads) == 0 {
				t.threadRefreshRecords = map[string]agentproto.ThreadSnapshotRecord{}
				return Result{
					Suppress: true,
					Events: []agentproto.Event{{
						Kind:    agentproto.EventThreadsSnapshot,
						Threads: nil,
					}},
				}, nil
			}
			var outbound [][]byte
			for index, thread := range threads {
				thread.ListOrder = index + 1
				t.threadRefreshRecords[thread.ThreadID] = thread
				t.threadRefreshOrder = append(t.threadRefreshOrder, thread.ThreadID)
				readID := t.nextRequest("thread-read")
				t.pendingThreadReads[readID] = thread.ThreadID
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
			return Result{Suppress: true, OutboundToCodex: outbound}, nil
		}
		if threadID, exists := t.pendingThreadReads[requestID]; exists {
			record := t.threadRefreshRecords[threadID]
			patch := parseThreadRecord(message["result"])
			record.ThreadID = choose(patch.ThreadID, record.ThreadID)
			record.Name = choose(patch.Name, record.Name)
			record.Preview = choose(patch.Preview, record.Preview)
			record.CWD = choose(patch.CWD, record.CWD)
			record.Loaded = record.Loaded || patch.Loaded
			record.Archived = record.Archived || patch.Archived
			record.State = choose(patch.State, record.State)
			t.threadRefreshRecords[threadID] = record
			delete(t.pendingThreadReads, requestID)
			if len(t.pendingThreadReads) == 0 {
				records := make([]agentproto.ThreadSnapshotRecord, 0, len(t.threadRefreshRecords))
				seen := map[string]bool{}
				for _, originalThreadID := range t.threadRefreshOrder {
					current, ok := t.threadRefreshRecords[originalThreadID]
					if !ok || current.ThreadID == "" || seen[current.ThreadID] {
						continue
					}
					records = append(records, current)
					seen[current.ThreadID] = true
				}
				extras := make([]agentproto.ThreadSnapshotRecord, 0, len(t.threadRefreshRecords))
				for _, current := range t.threadRefreshRecords {
					if current.ThreadID == "" || seen[current.ThreadID] {
						continue
					}
					extras = append(extras, current)
				}
				sort.Slice(extras, func(i, j int) bool {
					if extras[i].ListOrder != extras[j].ListOrder {
						return extras[i].ListOrder < extras[j].ListOrder
					}
					return strings.Compare(extras[i].ThreadID, extras[j].ThreadID) < 0
				})
				records = append(records, extras...)
				t.threadRefreshRecords = map[string]agentproto.ThreadSnapshotRecord{}
				t.threadRefreshOrder = nil
				return Result{
					Suppress: true,
					Events: []agentproto.Event{{
						Kind:    agentproto.EventThreadsSnapshot,
						Threads: records,
					}},
				}, nil
			}
			return Result{Suppress: true}, nil
		}
	}

	method, _ := message["method"].(string)
	switch method {
	case "thread/started":
		threadID := lookupString(message, "params", "thread", "id")
		if threadID == "" {
			threadID = lookupString(message, "params", "threadId")
		}
		cwd := lookupString(message, "params", "thread", "cwd")
		if cwd == "" {
			cwd = lookupString(message, "params", "cwd")
		}
		name := lookupString(message, "params", "thread", "name")
		if name == "" {
			name = lookupString(message, "params", "thread", "title")
		}
		if t.internalThreadIDs[threadID] {
			if cwd != "" {
				t.knownThreadCWD[threadID] = cwd
			}
			return Result{Events: []agentproto.Event{{
				Kind:         agentproto.EventThreadDiscovered,
				ThreadID:     threadID,
				CWD:          cwd,
				Name:         name,
				FocusSource:  "remote_created_thread",
				TrafficClass: agentproto.TrafficClassInternalHelper,
				Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
				Metadata:     map[string]any{"internalHelper": true},
			}}}, nil
		}
		t.currentThreadID = threadID
		if t.pendingLocalNewThreadTurn && threadID != "" {
			t.pendingLocalTurnByThread[threadID] = true
			t.pendingLocalNewThreadTurn = false
		}
		if cwd != "" {
			t.knownThreadCWD[threadID] = cwd
		}
		return Result{Events: []agentproto.Event{{
			Kind:        agentproto.EventThreadDiscovered,
			ThreadID:    threadID,
			CWD:         cwd,
			Name:        name,
			FocusSource: "remote_created_thread",
		}}}, nil
	case "thread/name/updated":
		threadID := lookupString(message, "params", "threadId")
		if t.internalThreadIDs[threadID] {
			name := lookupString(message, "params", "name")
			if name == "" {
				name = lookupString(message, "params", "thread", "name")
			}
			return Result{Events: []agentproto.Event{{
				Kind:         agentproto.EventThreadDiscovered,
				ThreadID:     threadID,
				Name:         name,
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
		}}}, nil
	case "turn/started":
		threadID := lookupString(message, "params", "thread", "id")
		if threadID == "" {
			threadID = lookupString(message, "params", "threadId")
		}
		turnID := lookupString(message, "params", "turn", "id")
		if turnID == "" {
			turnID = lookupString(message, "params", "turnId")
		}
		trafficClass := t.trafficClassForTurn(threadID, turnID)
		initiator := t.resolveTurnInitiator(threadID, turnID, trafficClass)
		if turnID != "" {
			t.turnInitiators[turnID] = initiator
		}
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventTurnStarted,
			ThreadID:     threadID,
			TurnID:       turnID,
			Status:       "running",
			TrafficClass: trafficClass,
			Initiator:    initiator,
		}}}, nil
	case "turn/completed":
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
		initiator := t.turnInitiators[turnID]
		if initiator.Kind == "" {
			initiator = t.resolveTurnInitiator(threadID, turnID, trafficClass)
		}
		delete(t.turnInitiators, turnID)
		delete(t.internalTurnIDs, turnID)
		return Result{Events: []agentproto.Event{{
			Kind:         agentproto.EventTurnCompleted,
			ThreadID:     threadID,
			TurnID:       turnID,
			Status:       status,
			ErrorMessage: errMsg,
			TrafficClass: trafficClass,
			Initiator:    initiator,
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
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     metadata,
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
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     extractItemMetadata(itemKind, item),
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

func (t *Translator) TranslateCommand(command agentproto.Command) ([][]byte, error) {
	switch command.Kind {
	case agentproto.CommandPromptSend:
		if command.Target.ThreadID == "" {
			t.pendingLocalNewThreadTurn = false
			requestID := t.nextRequest("thread-start")
			t.pendingThreadCreate[requestID] = pendingThreadCreate{Command: command}
			payload := map[string]any{
				"id":     requestID,
				"method": "thread/start",
				"params": t.buildThreadStartParams(command.Target.CWD, command.Overrides),
			}
			bytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return [][]byte{append(bytes, '\n')}, nil
		}
		delete(t.pendingLocalTurnByThread, command.Target.ThreadID)
		if t.currentThreadID == "" || command.Target.ThreadID != t.currentThreadID {
			requestID := t.nextRequest("thread-resume")
			t.pendingThreadResume[requestID] = pendingThreadResume{
				ThreadID: command.Target.ThreadID,
				Command:  command,
			}
			payload := map[string]any{
				"id":     requestID,
				"method": "thread/resume",
				"params": map[string]any{
					"threadId": command.Target.ThreadID,
					"cwd":      choose(command.Target.CWD, t.knownThreadCWD[command.Target.ThreadID]),
				},
			}
			bytes, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			return [][]byte{append(bytes, '\n')}, nil
		}
		payload, err := t.directTurnStart(command.Target.ThreadID, command, false)
		if err != nil {
			return nil, err
		}
		return [][]byte{payload}, nil
	case agentproto.CommandTurnInterrupt:
		payload := map[string]any{
			"id":     t.nextRequest("turn-interrupt"),
			"method": "turn/interrupt",
			"params": map[string]any{
				"threadId": command.Target.ThreadID,
				"turnId":   command.Target.TurnID,
			},
		}
		t.pendingSuppressedResponse[lookupStringFromAny(payload["id"])] = true
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandThreadsRefresh:
		requestID := t.nextRequest("threads-refresh")
		t.pendingThreadListRequestID = requestID
		payload := map[string]any{
			"id":     requestID,
			"method": "thread/list",
			"params": map[string]any{
				"limit":          50,
				"cursor":         nil,
				"sortKey":        "created_at",
				"modelProviders": []any{},
				"archived":       false,
				"sourceKinds":    []any{},
			},
		}
		bytes, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return [][]byte{append(bytes, '\n')}, nil
	case agentproto.CommandRequestRespond:
		return t.translateRequestRespond(command)
	default:
		return nil, nil
	}
}

func (t *Translator) translateRequestRespond(command agentproto.Command) ([][]byte, error) {
	if command.Request.RequestID == "" {
		return nil, nil
	}
	result := map[string]any{}
	responseType, _ := command.Request.Response["type"].(string)
	switch responseType {
	case "approval":
		approved, _ := command.Request.Response["approved"].(bool)
		if approved {
			result["decision"] = "accept"
		} else {
			result["decision"] = "decline"
		}
	case "structured":
		if value, ok := command.Request.Response["result"]; ok {
			result = map[string]any{"result": value}
		}
	default:
		result = command.Request.Response
	}
	payload := map[string]any{
		"id":     command.Request.RequestID,
		"result": result,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return [][]byte{append(bytes, '\n')}, nil
}

func (t *Translator) buildThreadStartParams(cwd string, overrides agentproto.PromptOverrides) map[string]any {
	params := cloneMap(t.latestThreadStartParams)
	if len(params) == 0 {
		params = map[string]any{}
	}
	params["cwd"] = choose(cwd, lookupStringFromAny(params["cwd"]))
	setDefault(params, "model", nil)
	setDefault(params, "modelProvider", nil)
	setDefault(params, "config", map[string]any{})
	setDefault(params, "approvalPolicy", "on-request")
	setDefault(params, "baseInstructions", nil)
	setDefault(params, "developerInstructions", nil)
	setDefault(params, "sandbox", "read-only")
	setDefault(params, "personality", nil)
	setDefault(params, "experimentalRawEvents", false)
	setDefault(params, "dynamicTools", nil)
	applyPromptOverridesToThreadStart(params, overrides)
	return params
}

func (t *Translator) directTurnStart(threadID string, command agentproto.Command, newThread bool) ([]byte, error) {
	delete(t.pendingLocalTurnByThread, threadID)
	t.pendingRemoteTurnByThread[threadID] = choose(command.Origin.Surface, command.Origin.ChatID)
	template := t.selectTurnTemplate(threadID, newThread)
	template["threadId"] = threadID
	template["input"] = t.buildInputs(command.Prompt.Inputs)
	template["cwd"] = choose(command.Target.CWD, choose(lookupStringFromAny(template["cwd"]), t.knownThreadCWD[threadID]))
	setDefault(template, "approvalPolicy", nil)
	setDefault(template, "sandboxPolicy", nil)
	setDefault(template, "model", nil)
	setDefault(template, "effort", nil)
	setDefault(template, "summary", "auto")
	setDefault(template, "personality", nil)
	setDefault(template, "collaborationMode", nil)
	setDefault(template, "attachments", []any{})
	applyPromptOverridesToTurnStart(template, command.Overrides)
	payload := map[string]any{
		"id":     t.nextRequest("turn-start"),
		"method": "turn/start",
		"params": template,
	}
	t.pendingSuppressedResponse[lookupStringFromAny(payload["id"])] = true
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append(bytes, '\n'), nil
}

func (t *Translator) selectTurnTemplate(threadID string, newThread bool) map[string]any {
	switch {
	case newThread && len(t.newThreadTurnTemplate) > 0:
		return cloneMap(t.newThreadTurnTemplate)
	case len(t.turnStartByThread[threadID]) > 0:
		return cloneMap(t.turnStartByThread[threadID])
	case len(t.latestTurnStartTemplate) > 0:
		return cloneMap(t.latestTurnStartTemplate)
	default:
		return map[string]any{}
	}
}

func (t *Translator) buildInputs(inputs []agentproto.Input) []map[string]any {
	output := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			output = append(output, map[string]any{"type": "text", "text": input.Text, "text_elements": []any{}})
		case agentproto.InputLocalImage:
			output = append(output, map[string]any{"type": "localImage", "path": input.Path, "mimeType": input.MIMEType})
		case agentproto.InputRemoteImage:
			output = append(output, map[string]any{"type": "image", "url": input.URL, "mimeType": input.MIMEType})
		}
	}
	return output
}

func configObservedEvents(threadID, cwd string, params map[string]any, treatAsDefault bool) []agentproto.Event {
	model, effort := extractObservedConfig(params)
	if model == "" && effort == "" {
		return nil
	}
	scope := "thread"
	if treatAsDefault || threadID == "" {
		scope = "cwd_default"
	}
	return []agentproto.Event{{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        threadID,
		CWD:             cwd,
		Model:           model,
		ReasoningEffort: effort,
		ConfigScope:     scope,
	}}
}

func extractObservedConfig(params map[string]any) (model, effort string) {
	model = choose(
		lookupString(params, "collaborationMode", "settings", "model"),
		lookupStringFromAny(params["model"]),
		lookupString(params, "config", "model"),
	)
	effort = choose(
		lookupString(params, "collaborationMode", "settings", "reasoning_effort"),
		lookupString(params, "config", "model_reasoning_effort"),
		lookupString(params, "config", "reasoning_effort"),
		lookupStringFromAny(params["effort"]),
	)
	return model, effort
}

func applyPromptOverridesToThreadStart(params map[string]any, overrides agentproto.PromptOverrides) {
	if overrides.Model == "" && overrides.ReasoningEffort == "" {
		return
	}
	if overrides.Model != "" {
		params["model"] = overrides.Model
	}
	if overrides.ReasoningEffort != "" {
		configMap := lookupMapFromAny(params["config"])
		configMap["model_reasoning_effort"] = overrides.ReasoningEffort
		configMap["reasoning_effort"] = overrides.ReasoningEffort
		params["config"] = configMap
	}
}

func applyPromptOverridesToTurnStart(template map[string]any, overrides agentproto.PromptOverrides) {
	if overrides.Model == "" && overrides.ReasoningEffort == "" {
		return
	}
	if overrides.Model != "" {
		template["model"] = overrides.Model
	}
	if overrides.ReasoningEffort != "" {
		template["effort"] = overrides.ReasoningEffort
	}
	collaborationMode := lookupMapFromAny(template["collaborationMode"])
	if len(collaborationMode) == 0 {
		collaborationMode = map[string]any{"mode": "custom"}
	}
	settings := lookupMapFromAny(collaborationMode["settings"])
	if overrides.Model != "" {
		settings["model"] = overrides.Model
	}
	if overrides.ReasoningEffort != "" {
		settings["reasoning_effort"] = overrides.ReasoningEffort
	}
	collaborationMode["settings"] = settings
	template["collaborationMode"] = collaborationMode
}

func (t *Translator) nextRequest(prefix string) string {
	value := fmt.Sprintf("relay-%s-%d", prefix, t.nextID)
	t.nextID++
	return value
}

func normalizeThreadStartParams(params map[string]any) map[string]any {
	normalized := cloneMap(params)
	delete(normalized, "ephemeral")
	delete(normalized, "persistExtendedHistory")
	setDefault(normalized, "cwd", nil)
	setDefault(normalized, "model", nil)
	setDefault(normalized, "modelProvider", nil)
	setDefault(normalized, "config", map[string]any{})
	setDefault(normalized, "approvalPolicy", "on-request")
	setDefault(normalized, "baseInstructions", nil)
	setDefault(normalized, "developerInstructions", nil)
	setDefault(normalized, "sandbox", "read-only")
	setDefault(normalized, "personality", nil)
	setDefault(normalized, "experimentalRawEvents", false)
	setDefault(normalized, "dynamicTools", nil)
	return normalized
}

func normalizeTurnStartTemplate(params map[string]any) map[string]any {
	normalized := map[string]any{}
	for _, key := range []string{
		"cwd",
		"approvalPolicy",
		"sandboxPolicy",
		"model",
		"effort",
		"summary",
		"personality",
		"collaborationMode",
		"attachments",
	} {
		if value, ok := params[key]; ok {
			normalized[key] = value
		}
	}
	setDefault(normalized, "summary", "auto")
	setDefault(normalized, "attachments", []any{})
	return normalized
}

func isInternalLocalThreadStart(params map[string]any) bool {
	if lookupBoolFromAny(params["ephemeral"]) {
		return true
	}
	value, ok := params["persistExtendedHistory"].(bool)
	return ok && !value
}

func isInternalLocalTurnStart(params map[string]any) bool {
	return !isNull(params["outputSchema"])
}

func (t *Translator) trafficClassForTurn(threadID, turnID string) agentproto.TrafficClass {
	switch {
	case turnID != "" && t.internalTurnIDs[turnID]:
		return agentproto.TrafficClassInternalHelper
	default:
		return agentproto.TrafficClassPrimary
	}
}

func (t *Translator) resolveTurnInitiator(threadID, turnID string, trafficClass agentproto.TrafficClass) agentproto.Initiator {
	if trafficClass == agentproto.TrafficClassInternalHelper {
		return agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
	}
	if surfaceID := t.pendingRemoteTurnByThread[threadID]; surfaceID != "" {
		delete(t.pendingRemoteTurnByThread, threadID)
		delete(t.pendingLocalTurnByThread, threadID)
		return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID}
	}
	if t.pendingLocalTurnByThread[threadID] {
		delete(t.pendingLocalTurnByThread, threadID)
		return agentproto.Initiator{Kind: agentproto.InitiatorLocalUI}
	}
	if turnID != "" {
		if initiator := t.turnInitiators[turnID]; initiator.Kind != "" {
			return initiator
		}
	}
	return agentproto.Initiator{Kind: agentproto.InitiatorUnknown}
}

func (t *Translator) initiatorForTurn(threadID, turnID string) agentproto.Initiator {
	if turnID != "" {
		if initiator := t.turnInitiators[turnID]; initiator.Kind != "" {
			return initiator
		}
	}
	if t.trafficClassForTurn(threadID, turnID) == agentproto.TrafficClassInternalHelper {
		return agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
	}
	return agentproto.Initiator{}
}

func parseThreadList(result any) []agentproto.ThreadSnapshotRecord {
	var raw []any
	switch value := result.(type) {
	case map[string]any:
		switch current := value["threads"].(type) {
		case []any:
			raw = current
		}
		if len(raw) == 0 {
			switch current := value["data"].(type) {
			case []any:
				raw = current
			}
		}
	case []any:
		raw = value
	}
	output := make([]agentproto.ThreadSnapshotRecord, 0, len(raw))
	for index, current := range raw {
		switch item := current.(type) {
		case string:
			output = append(output, agentproto.ThreadSnapshotRecord{ThreadID: item, Loaded: true, ListOrder: index + 1})
		case map[string]any:
			record := parseThreadRecord(item)
			record.Loaded = true
			record.ListOrder = index + 1
			if record.ThreadID != "" {
				output = append(output, record)
			}
		}
	}
	return output
}

func parseThreadRecord(result any) agentproto.ThreadSnapshotRecord {
	var object map[string]any
	switch value := result.(type) {
	case map[string]any:
		if thread, ok := value["thread"].(map[string]any); ok {
			object = thread
		} else {
			object = value
		}
	default:
		return agentproto.ThreadSnapshotRecord{}
	}
	return agentproto.ThreadSnapshotRecord{
		ThreadID: choose(
			lookupStringFromAny(object["id"]),
			lookupStringFromAny(object["threadId"]),
		),
		Name: choose(
			lookupStringFromAny(object["name"]),
			lookupStringFromAny(object["title"]),
		),
		Preview: choose(
			lookupStringFromAny(object["preview"]),
			lookupStringFromAny(object["summary"]),
		),
		CWD: choose(
			lookupStringFromAny(object["cwd"]),
			lookupStringFromAny(object["path"]),
		),
		Model: choose(
			lookupString(object, "latestCollaborationMode", "settings", "model"),
			lookupString(object, "collaborationMode", "settings", "model"),
			lookupStringFromAny(object["model"]),
		),
		ReasoningEffort: choose(
			lookupString(object, "latestCollaborationMode", "settings", "reasoning_effort"),
			lookupString(object, "collaborationMode", "settings", "reasoning_effort"),
			lookupString(object, "config", "model_reasoning_effort"),
			lookupString(object, "config", "reasoning_effort"),
			lookupStringFromAny(object["effort"]),
		),
		Loaded:   lookupBoolFromAny(object["loaded"]),
		Archived: lookupBoolFromAny(object["archived"]),
		State:    lookupStringFromAny(object["state"]),
		ListOrder: lookupIntFromAny(chooseAny(
			object["listOrder"],
			object["list_order"],
		)),
	}
}

func chooseAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func setDefault(target map[string]any, key string, value any) {
	if _, exists := target[key]; !exists {
		target[key] = value
	}
}

func isNull(value any) bool {
	return value == nil
}

func lookupString(value map[string]any, path ...string) string {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[part]
	}
	return lookupStringFromAny(current)
}

func lookupAny(value map[string]any, path ...string) any {
	var current any = value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func lookupMap(value map[string]any, path ...string) map[string]any {
	current, _ := lookupAny(value, path...).(map[string]any)
	return current
}

func lookupMapFromAny(value any) map[string]any {
	current, _ := value.(map[string]any)
	if current == nil {
		return map[string]any{}
	}
	return cloneMap(current)
}

func lookupStringFromAny(value any) string {
	switch current := value.(type) {
	case string:
		return current
	default:
		return ""
	}
}

func lookupIntFromAny(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int32:
		return int(current)
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}

func lookupBoolFromAny(value any) bool {
	current, _ := value.(bool)
	return current
}

func choose(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeItemKind(raw string) string {
	switch raw {
	case "agentMessage", "assistant_message", "assistantMessage":
		return "agent_message"
	case "userMessage", "user_message":
		return "user_message"
	case "plan":
		return "plan"
	case "reasoning":
		return "reasoning"
	case "commandExecution", "command_execution":
		return "command_execution"
	case "fileChange", "file_change":
		return "file_change"
	case "mcpToolCall", "mcp_tool_call":
		return "mcp_tool_call"
	case "dynamicToolCall", "dynamic_tool_call":
		return "dynamic_tool_call"
	case "collabAgentToolCall", "collab_agent_tool_call":
		return "collab_agent_tool_call"
	default:
		return raw
	}
}

func extractItemMetadata(itemKind string, item map[string]any) map[string]any {
	metadata := map[string]any{}
	if item == nil {
		return metadata
	}
	if text := extractItemText(item); text != "" {
		metadata["text"] = text
	}
	switch itemKind {
	case "reasoning":
		if summary := extractStringList(item["summary"]); len(summary) > 0 {
			metadata["summary"] = summary
		}
		if content := extractStringList(item["content"]); len(content) > 0 {
			metadata["content"] = content
		}
	}
	return metadata
}

func extractItemText(item map[string]any) string {
	if text := lookupStringFromAny(item["text"]); text != "" {
		return text
	}
	content, _ := item["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(content))
	for _, current := range content {
		entry, _ := current.(map[string]any)
		if entry == nil {
			continue
		}
		if lookupStringFromAny(entry["type"]) != "text" {
			continue
		}
		if text := lookupStringFromAny(entry["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractStringList(value any) []string {
	raw, _ := value.([]any)
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, current := range raw {
		if text := lookupStringFromAny(current); text != "" {
			out = append(out, text)
		}
	}
	return out
}
