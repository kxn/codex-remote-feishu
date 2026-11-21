package wrapper

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type inspectedTurn struct {
	Binding   agentproto.ExecutionBinding
	CommandID string
	Active    bool
	Terminal  bool
}
type workInspectionState struct {
	startupProcessUntracked bool

	scopeID              string
	cliSession           string
	dirty                bool
	rpcMethods           map[string]string
	rpcSessions          map[string]string
	sessionIDs           map[string]bool
	persistMu            sync.Mutex
	activeNativeCommands map[string]bool
	inputRPCMethods      map[string]string
	receiptEvents        []agentproto.Event
	nativeReady          bool
	bootstrap            agentproto.RPCProbe
	goalKnown            map[string]bool
	goalActive           map[string]bool
	mu                   sync.Mutex
	scopes               []*hostinventory.Scope
	turns                map[string]inspectedTurn
	pendingInputs        map[string]string
	activeTools          map[string]bool
	tasks                map[string]bool
	uncertainties        map[string]bool
	waiters              map[string]chan []byte
	taskInventoryKnown   bool
}

func newWorkInspectionState() *workInspectionState {
	return &workInspectionState{turns: make(map[string]inspectedTurn), pendingInputs: make(map[string]string), activeTools: make(map[string]bool), tasks: make(map[string]bool), uncertainties: make(map[string]bool), waiters: make(map[string]chan []byte), taskInventoryKnown: true, goalKnown: make(map[string]bool), rpcMethods: make(map[string]string), rpcSessions: make(map[string]string), sessionIDs: make(map[string]bool), activeNativeCommands: make(map[string]bool), inputRPCMethods: make(map[string]string), goalActive: make(map[string]bool)}
}
func nativeTurnKey(session, turn string) string { return session + "\x00" + turn }

func (s *workInspectionState) outbound(commandID string, frame []byte) {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
	if message["type"] == "user" {
		if uuid, ok := message["uuid"].(string); ok {
			s.pendingInputs["uuid:"+uuid] = commandID
		}
		return
	}
	method, _ := message["method"].(string)
	if method != "" {
		if id, ok := message["id"]; ok {
			key := fmt.Sprint(id)
			s.rpcMethods[key] = method
			params, _ := message["params"].(map[string]any)
			session, _ := params["threadId"].(string)
			s.rpcSessions[key] = session
		}
	}
	if method == "turn/start" || method == "turn/steer" || method == "session/prompt" {
		if id, exists := message["id"]; exists {
			s.pendingInputs["rpc:"+fmt.Sprint(id)] = commandID
			s.inputRPCMethods[fmt.Sprint(id)] = method
		}
	}
}

func (s *workInspectionState) events(events []agentproto.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventTurnCompleted || event.Kind == agentproto.EventNativeTurnBindingObserved || event.Kind == agentproto.EventNativeInputReceipt {
			s.dirty = true
		}
		if event.ThreadID != "" && (event.Kind == agentproto.EventNativeTurnBindingObserved || event.Kind == agentproto.EventTurnStarted || (event.Kind == agentproto.EventTurnCompleted && event.TurnID != "")) {
			s.sessionIDs[event.ThreadID] = true
		}
		key := nativeTurnKey(event.ThreadID, event.TurnID)
		proof := s.turns[key]
		if event.Kind == agentproto.EventNativeTurnBindingObserved || event.Kind == agentproto.EventTurnStarted {
			if event.Execution != nil {
				proof.Binding = *event.Execution
				proof.CommandID = event.CommandID
			}
			if !proof.Terminal {
				proof.Active = true
			}
			s.turns[key] = proof
		}
		if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventTurnCompleted {
			for key, commandID := range s.pendingInputs {
				if commandID == event.CommandID && commandID != "" {
					delete(s.pendingInputs, key)
				}
			}
		}
		if event.Kind == agentproto.EventTurnCompleted {
			prefix := s.scopeID + "\x00" + event.ThreadID + "\x00"
			for key := range s.activeTools {
				if strings.HasPrefix(key, prefix) {
					delete(s.activeTools, key)
				}
			}
			proof.Active = false
			proof.Terminal = true
			s.turns[key] = proof
			delete(s.activeNativeCommands, event.CommandID)
			delete(s.activeNativeCommands, proof.CommandID)
		}
		if event.Kind == agentproto.EventRequestStarted && event.RequestID != "" {
			s.pendingInputs["request:"+event.RequestID] = event.CommandID
			s.dirty = true
		}
		if event.Kind == agentproto.EventRequestResolved {
			delete(s.pendingInputs, "request:"+event.RequestID)
			s.dirty = true
		}
		if event.Kind == agentproto.EventNativeInputReceipt && event.Status == "started" && event.TurnID != "" && event.Execution != nil {
			proof.Binding = *event.Execution
			proof.CommandID = event.CommandID
			proof.Active = !proof.Terminal
			s.turns[key] = proof
			delete(s.activeNativeCommands, event.CommandID)
		}
		if event.Kind == agentproto.EventNativeInputReceipt && (event.Status == "started" || event.Status == "cancelled" || event.Status == "completed") {
			delete(s.pendingInputs, "uuid:"+event.RequestID)
		}
	}
}

var escapingNativeCommand = regexp.MustCompile(`(?im)(^|[;&|]|\$\()\s*(exec\s+|command\s+)?([^\s;|]*/)?(setsid|daemonize|ssh)([\s;|]|$)|(^|[;&|])\s*(docker|podman|nerdctl)\s+[^;|]*(--detach|\s-d\b)|(^|[;&|])\s*(systemctl|launchctl)\s+(start|load|bootstrap)|(^|[;&|])\s*start-process([\s;|]|$)`)

func (s *workInspectionState) raw(frame []byte) bool {
	var message map[string]any
	if json.Unmarshal(frame, &message) != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if requestID := nativeResponseRequestID(frame); requestID != "" {
		method := s.rpcMethods[requestID]
		session := s.rpcSessions[requestID]
		result, _ := message["result"].(map[string]any)
		if method == "session/new" || method == "session/resume" || method == "session/fork" {
			if id, ok := result["sessionId"].(string); ok && id != "" {
				s.sessionIDs[id] = true
				s.dirty = true
			}
		}
		if method == "thread/start" || method == "thread/resume" {
			if thread, ok := result["thread"].(map[string]any); ok {
				if id, ok := thread["id"].(string); ok && id != "" {
					s.sessionIDs[id] = true
					if _, known := s.goalKnown[id]; !known {
						s.goalKnown[id] = false
					}
					s.dirty = true
				}
			}
		}
		if method == "thread/goal/get" || method == "thread/goal/set" {
			if goal, exists := result["goal"]; exists {
				if goal == nil {
					s.goalKnown[session] = true
					s.goalActive[session] = false
				} else if record, ok := goal.(map[string]any); ok {
					status, _ := record["status"].(string)
					s.goalKnown[session] = status == "active" || status == "paused" || status == "completed" || status == "budget_exhausted"
					s.goalActive[session] = status == "active"
				}
				s.dirty = true
			}
		}
		if method == "thread/goal/clear" && result["cleared"] == true {
			s.goalKnown[session] = true
			s.goalActive[session] = false
			s.dirty = true
		}
		delete(s.rpcMethods, requestID)
		delete(s.rpcSessions, requestID)
		if commandID, known := s.pendingInputs["rpc:"+requestID]; known {
			method := s.inputRPCMethods[requestID]
			status := "accepted"
			if _, failed := message["error"]; failed {
				status = "rejected"
			} else if method == "turn/start" {
				s.activeNativeCommands[commandID] = true
			}
			s.receiptEvents = append(s.receiptEvents, agentproto.Event{Kind: agentproto.EventNativeInputReceipt, CommandID: commandID, RequestID: requestID, Status: status, Action: "native." + method})
			s.dirty = true
			delete(s.inputRPCMethods, requestID)
		}
		delete(s.pendingInputs, "rpc:"+requestID)
		if wait, ok := s.waiters[requestID]; ok {
			select {
			case wait <- append([]byte(nil), frame...):
			default:
			}
			delete(s.waiters, requestID)
			return true
		}
	}
	if message["type"] == "command_lifecycle" {
		s.dirty = true
		uuid, _ := message["command_uuid"].(string)
		state, _ := message["state"].(string)
		if state == "started" || state == "cancelled" || state == "completed" {
			if commandID, known := s.pendingInputs["uuid:"+uuid]; known {
				if state == "started" {
					s.activeNativeCommands[commandID] = true
				} else {
					delete(s.activeNativeCommands, commandID)
				}
			}
			delete(s.pendingInputs, "uuid:"+uuid)
		}
	}
	method, _ := message["method"].(string)
	sessionScope, _ := message["session_id"].(string)
	if params, ok := message["params"].(map[string]any); ok {
		if id, ok := params["sessionId"].(string); ok {
			sessionScope = id
		}
		if id, ok := params["threadId"].(string); ok {
			sessionScope = id
		} else if thread, ok := params["thread"].(map[string]any); ok {
			sessionScope, _ = thread["id"].(string)
		}
	}
	if sessionScope == "" {
		sessionScope = s.cliSession
	}
	toolNamespace := s.scopeID + "\x00" + sessionScope + "\x00"
	if method == "session/update" {
		params, _ := message["params"].(map[string]any)
		update, _ := params["update"].(map[string]any)
		kind, _ := update["sessionUpdate"].(string)
		if kind == "tool_call" || kind == "tool_call_update" {
			id, _ := update["toolCallId"].(string)
			status, _ := update["status"].(string)
			if id == "" {
				s.taskInventoryKnown = false
			} else if status == "completed" || status == "failed" {
				delete(s.activeTools, toolNamespace+id)
			} else {
				s.activeTools[toolNamespace+id] = true
			}
			if raw, ok := update["rawInput"].(map[string]any); ok {
				if command, ok := raw["command"].(string); ok {
					s.observeEscape(toolNamespace+id, command)
				}
			}
			s.dirty = true
		}
	}
	if method == "item/started" || method == "item/completed" {
		s.dirty = true
		params, _ := message["params"].(map[string]any)
		item, _ := params["item"].(map[string]any)
		id, _ := item["id"].(string)
		kind, _ := item["type"].(string)
		if id != "" && (kind == "commandExecution" || kind == "mcpToolCall" || kind == "dynamicToolCall") {
			if method == "item/started" {
				s.activeTools[toolNamespace+id] = true
			} else {
				delete(s.activeTools, toolNamespace+id)
			}
			command, _ := item["command"].(string)
			s.observeEscape(toolNamespace+id, command)
			if processID, ok := item["processId"].(string); ok && processID != "" {
				if item["status"] == "inProgress" {
					s.tasks["codex-process:"+toolNamespace+processID] = true
				} else if _, hasExit := item["exitCode"]; hasExit && item["exitCode"] != nil {
					delete(s.tasks, "codex-process:"+toolNamespace+processID)
				}
			}
		}
	}
	if message["type"] == "assistant" {
		body, _ := message["message"].(map[string]any)
		blocks, _ := body["content"].([]any)
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			if block["type"] != "tool_use" {
				continue
			}
			s.dirty = true
			id, _ := block["id"].(string)
			name, _ := block["name"].(string)
			if id == "" {
				s.taskInventoryKnown = false
				continue
			}
			s.activeTools[toolNamespace+id] = true
			input, _ := block["input"].(map[string]any)
			command, _ := input["command"].(string)
			s.observeEscape(toolNamespace+id, command)
			if name == "Bash" || name == "Task" || name == "Agent" {
				if background, ok := input["run_in_background"].(bool); ok && background {
					s.tasks["claude-tool:"+toolNamespace+id] = true
				}
			}
		}
	}
	if message["type"] == "user" {
		body, _ := message["message"].(map[string]any)
		blocks, _ := body["content"].([]any)
		for _, raw := range blocks {
			block, _ := raw.(map[string]any)
			if block["type"] != "tool_result" {
				continue
			}
			s.dirty = true
			id, _ := block["tool_use_id"].(string)
			delete(s.activeTools, toolNamespace+id)
			if result, ok := message["tool_use_result"].(map[string]any); ok {
				found := false
				for _, field := range []string{"task_id", "backgroundTaskId", "background_task_id", "shell_id"} {
					if taskID, ok := result[field].(string); ok && taskID != "" {
						s.tasks["claude-task:"+toolNamespace+taskID] = true
						found = true
					}
				}
				if found {
					delete(s.tasks, "claude-tool:"+toolNamespace+id)
				} else if result["status"] == "completed" {
					delete(s.tasks, "claude-tool:"+toolNamespace+id)
				}
			}
		}
	}
	if message["type"] == "system" {
		if message["subtype"] == "init" {
			if session, ok := message["session_id"].(string); ok && session != "" {
				s.sessionIDs[session] = true
				s.cliSession = session
				s.goalKnown[session] = true
				s.goalActive[session] = false
				s.dirty = true
			}
		}
		subtype, _ := message["subtype"].(string)
		if subtype == "task_started" || subtype == "task_notification" {
			s.dirty = true
			taskID, _ := message["task_id"].(string)
			if taskID == "" {
				s.taskInventoryKnown = false
			} else {
				key := "claude-task:" + toolNamespace + taskID
				if subtype == "task_started" {
					s.tasks[key] = true
				} else {
					status, _ := message["status"].(string)
					if status == "completed" || status == "failed" || status == "stopped" {
						delete(s.tasks, key)
					}
				}
			}
		}
	}
	return false
}

func (s *workInspectionState) observeEscape(id, command string) {
	if escapingNativeCommand.MatchString(command) {
		s.uncertainties["native-tool:"+id+":external-or-escaped-work"] = true
	}
}

func (s *workInspectionState) inspectInventory(snapshot agentproto.RuntimeLaunchSnapshot) agentproto.BackgroundInspection {
	s.mu.Lock()
	scopes := append([]*hostinventory.Scope(nil), s.scopes...)
	result := agentproto.BackgroundInspection{CarrierIncarnation: snapshot.CarrierIncarnation, ProcessGeneration: snapshot.ProcessGeneration, LocationGeneration: snapshot.LocationGeneration, NativeInputsComplete: true, NativeTaskSetComplete: s.taskInventoryKnown, PendingNativeInputs: len(s.pendingInputs)}
	for commandID := range s.activeNativeCommands {
		result.ActiveNativeTurnIDs = append(result.ActiveNativeTurnIDs, "native-input:"+commandID)
	}
	for key, proof := range s.turns {
		if proof.Active {
			result.ActiveNativeTurnIDs = append(result.ActiveNativeTurnIDs, key)
		}
	}
	result.ContinuationInventoryKnown = true
	for session, known := range s.goalKnown {
		if !known {
			result.ContinuationInventoryKnown = false
		}
		if s.goalActive[session] {
			result.ActiveGoalSessionIDs = append(result.ActiveGoalSessionIDs, session)
		}
	}
	for id := range s.tasks {
		result.PendingTaskIDs = append(result.PendingTaskIDs, id)
	}
	for id := range s.activeTools {
		result.PendingTaskIDs = append(result.PendingTaskIDs, "active-tool:"+id)
	}
	for id := range s.uncertainties {
		result.ExternalResourceUncertainties = append(result.ExternalResourceUncertainties, id)
	}
	s.mu.Unlock()
	complete := len(scopes) > 0 && len(result.ExternalResourceUncertainties) == 0
	var observed []hostinventory.Snapshot
	for _, scope := range scopes {
		record := scope.Inspect()
		observed = append(observed, record)
		if !record.Complete {
			complete = false
		}
		for _, pid := range record.ProcessIDs {
			result.RunningProcessIDs = append(result.RunningProcessIDs, fmt.Sprint(pid))
		}
	}
	result.Complete = complete
	result.Evidence, _ = json.Marshal(map[string]any{"scope": "managed_native_process_groups_or_jobs", "process_scopes": observed, "external_resources": result.ExternalResourceUncertainties})
	return result
}

func (s *workInspectionState) drainReceipts() []agentproto.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.receiptEvents
	s.receiptEvents = nil
	return events
}
