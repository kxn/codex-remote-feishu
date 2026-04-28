package mockcodex

import (
	"encoding/json"
	"fmt"
	"strings"
)

type MockCodex struct {
	nextThreadID           int
	nextTurnID             int
	nextItemID             int
	Threads                map[string]*Thread
	FocusedThreadID        string
	ActiveTurn             *Turn
	AutoComplete           bool
	EmitItemDeltas         bool
	OmitFinalText          bool
	RequireInitialize      bool
	InitializeSeen         bool
	Initialized            bool
	ExitAfterFinalOutput   bool
	ExitAfterInterrupt     bool
	ExitAfterOutputCode    int
	ExitAfterInterruptCode int
	LastTurnStart          map[string]any
	Responder              func(turn TurnStart) string

	scheduledExit     bool
	scheduledExitCode int
}

type Thread struct {
	ID   string
	CWD  string
	Name string
}

type Turn struct {
	ID       string
	ThreadID string
}

type TurnStart struct {
	ThreadID string
	CWD      string
	Inputs   []map[string]any
}

func New() *MockCodex {
	return &MockCodex{
		Threads:        map[string]*Thread{},
		AutoComplete:   true,
		EmitItemDeltas: true,
		Responder: func(turn TurnStart) string {
			for _, input := range turn.Inputs {
				if text, _ := input["text"].(string); text != "" {
					return "已收到：\n\n```text\n" + text + "\n```"
				}
			}
			return "已收到请求。"
		},
	}
}

func (m *MockCodex) SeedThread(id, cwd, name string) {
	m.Threads[id] = &Thread{ID: id, CWD: cwd, Name: name}
	m.FocusedThreadID = id
}

func (m *MockCodex) HandleRemoteCommand(raw []byte) ([][]byte, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	method, _ := message["method"].(string)
	id := fmt.Sprint(message["id"])
	params, _ := message["params"].(map[string]any)

	switch method {
	case "initialize":
		if m.InitializeSeen {
			return [][]byte{
				mustJSON(map[string]any{"id": id, "error": map[string]any{
					"code":    -32600,
					"message": "Already initialized",
				}}),
			}, nil
		}
		m.InitializeSeen = true
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{
				"userAgent":      "mockcodex/0.0.1",
				"codexHome":      "/tmp/mockcodex",
				"platformFamily": "unix",
				"platformOs":     "linux",
			}}),
		}, nil
	case "initialized":
		if !m.InitializeSeen {
			return nil, nil
		}
		m.Initialized = true
		return nil, nil
	case "thread/start":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		m.nextThreadID++
		threadID := fmt.Sprintf("thread-%d", m.nextThreadID)
		cwd, _ := params["cwd"].(string)
		m.Threads[threadID] = &Thread{ID: threadID, CWD: cwd, Name: "新会话"}
		m.FocusedThreadID = threadID
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": threadID}}}),
			mustJSON(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": threadID, "cwd": cwd, "name": "新会话"}}}),
		}, nil
	case "thread/resume":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		threadID, _ := params["threadId"].(string)
		thread := m.Threads[threadID]
		if thread == nil {
			thread = &Thread{ID: threadID}
			m.Threads[threadID] = thread
		}
		if cwd, _ := params["cwd"].(string); cwd != "" {
			thread.CWD = cwd
		}
		m.FocusedThreadID = threadID
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{}}),
			mustJSON(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": thread.ID, "cwd": thread.CWD, "name": thread.Name}}}),
		}, nil
	case "thread/loaded/list":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		items := make([]map[string]any, 0, len(m.Threads))
		for _, thread := range m.Threads {
			items = append(items, map[string]any{
				"id":      thread.ID,
				"name":    thread.Name,
				"cwd":     thread.CWD,
				"loaded":  true,
				"preview": "",
			})
		}
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"threads": items}}),
		}, nil
	case "thread/list":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		items := make([]map[string]any, 0, len(m.Threads))
		for _, thread := range m.Threads {
			items = append(items, map[string]any{
				"id":      thread.ID,
				"name":    thread.Name,
				"cwd":     thread.CWD,
				"preview": "",
				"state":   "",
			})
		}
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"data": items}}),
		}, nil
	case "thread/read":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		threadID, _ := params["threadId"].(string)
		thread := m.Threads[threadID]
		if thread == nil {
			return [][]byte{mustJSON(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": threadID}}})}, nil
		}
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{
				"id":      thread.ID,
				"name":    thread.Name,
				"cwd":     thread.CWD,
				"loaded":  true,
				"preview": "",
			}}}),
		}, nil
	case "thread/name/set":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		threadID, _ := params["threadId"].(string)
		name, _ := params["name"].(string)
		thread := m.Threads[threadID]
		if thread == nil {
			thread = &Thread{ID: threadID}
			m.Threads[threadID] = thread
		}
		thread.Name = name
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"ok": true}}),
			mustJSON(map[string]any{"method": "thread/name/updated", "params": map[string]any{"threadId": threadID, "name": name}}),
		}, nil
	case "turn/start":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		threadID, _ := params["threadId"].(string)
		cwd, _ := params["cwd"].(string)
		inputs, _ := params["input"].([]any)
		normalized := make([]map[string]any, 0, len(inputs))
		for _, input := range inputs {
			object, _ := input.(map[string]any)
			normalized = append(normalized, object)
		}
		threadID = m.resolveRemoteThread(threadID)
		m.LastTurnStart = params
		m.nextTurnID++
		m.nextItemID++
		turnID := fmt.Sprintf("turn-%d", m.nextTurnID)
		itemID := fmt.Sprintf("item-%d", m.nextItemID)
		m.ActiveTurn = &Turn{ID: turnID, ThreadID: threadID}
		if thread := m.Threads[threadID]; thread != nil && cwd == "" {
			cwd = thread.CWD
		}
		response := m.Responder(TurnStart{ThreadID: threadID, CWD: cwd, Inputs: normalized})

		outputs := [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": turnID}}}),
			mustJSON(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID}}}),
		}
		if m.AutoComplete {
			outputs = append(outputs, m.completeTurnFrames(threadID, turnID, itemID, response)...)
			if m.ExitAfterFinalOutput {
				outputs = stripTrailingTurnCompleted(outputs)
				m.scheduleExit(m.ExitAfterOutputCode)
			}
			m.ActiveTurn = nil
		}
		return outputs, nil
	case "turn/interrupt":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		if m.ActiveTurn == nil {
			return [][]byte{mustJSON(map[string]any{"id": id, "result": map[string]any{}})}, nil
		}
		turn := m.ActiveTurn
		m.ActiveTurn = nil
		if m.ExitAfterInterrupt {
			m.scheduleExit(m.ExitAfterInterruptCode)
			return [][]byte{
				mustJSON(map[string]any{"id": id, "result": map[string]any{}}),
			}, nil
		}
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{}}),
			mustJSON(map[string]any{"method": "turn/completed", "params": map[string]any{
				"threadId": turn.ThreadID,
				"turn":     map[string]any{"id": turn.ID, "status": "interrupted", "error": nil},
			}}),
		}, nil
	case "turn/steer":
		if outputs := m.requireInitialized(id); outputs != nil {
			return outputs, nil
		}
		if m.ActiveTurn == nil {
			return [][]byte{
				mustJSON(map[string]any{"id": id, "error": map[string]any{
					"code":    -32600,
					"message": "No active turn",
				}}),
			}, nil
		}
		expectedTurnID, _ := params["expectedTurnId"].(string)
		if expectedTurnID == "" {
			return [][]byte{
				mustJSON(map[string]any{"id": id, "error": map[string]any{
					"code":    -32600,
					"message": "Invalid request: missing field `expectedTurnId`",
				}}),
			}, nil
		}
		if expectedTurnID != m.ActiveTurn.ID {
			return [][]byte{
				mustJSON(map[string]any{"id": id, "error": map[string]any{
					"code":    -32600,
					"message": fmt.Sprintf("expected active turn id `%s` but found `%s`", expectedTurnID, m.ActiveTurn.ID),
				}}),
			}, nil
		}
		inputs, _ := params["input"].([]any)
		text := ""
		for _, input := range inputs {
			object, _ := input.(map[string]any)
			if current, _ := object["text"].(string); current != "" {
				text = current
			}
		}
		m.nextItemID++
		return [][]byte{
			mustJSON(map[string]any{"id": id, "result": map[string]any{}}),
			mustJSON(map[string]any{"method": "item/started", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"item":     map[string]any{"id": fmt.Sprintf("item-%d", m.nextItemID), "type": "agentMessage"},
			}}),
			mustJSON(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"itemId":   fmt.Sprintf("item-%d", m.nextItemID),
				"delta":    "追加输入：" + strings.TrimSpace(text),
			}}),
			mustJSON(map[string]any{"method": "item/completed", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"item":     map[string]any{"id": fmt.Sprintf("item-%d", m.nextItemID), "type": "agentMessage", "text": "追加输入：" + strings.TrimSpace(text)},
			}}),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported remote method: %s", method)
	}
}

func (m *MockCodex) requireInitialized(id string) [][]byte {
	if !m.RequireInitialize || m.Initialized {
		return nil
	}
	return [][]byte{
		mustJSON(map[string]any{"id": id, "error": map[string]any{
			"code":    -32600,
			"message": "Not initialized",
		}}),
	}
}

func (m *MockCodex) HandleLocalClientMessage(raw []byte) ([][]byte, error) {
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	method, _ := message["method"].(string)
	params, _ := message["params"].(map[string]any)
	switch method {
	case "thread/resume":
		threadID, _ := params["threadId"].(string)
		cwd, _ := params["cwd"].(string)
		thread := m.Threads[threadID]
		if thread == nil {
			thread = &Thread{ID: threadID, CWD: cwd}
			m.Threads[threadID] = thread
		}
		m.FocusedThreadID = threadID
		return [][]byte{mustJSON(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": threadID, "cwd": choose(cwd, thread.CWD)}}})}, nil
	case "turn/start":
		return m.handleLocalTurn(fmt.Sprint(message["id"]), params)
	case "turn/steer":
		if m.ActiveTurn == nil {
			return [][]byte{
				mustJSON(map[string]any{"id": fmt.Sprint(message["id"]), "error": map[string]any{
					"code":    -32600,
					"message": "No active turn",
				}}),
			}, nil
		}
		expectedTurnID, _ := params["expectedTurnId"].(string)
		if expectedTurnID == "" {
			return [][]byte{
				mustJSON(map[string]any{"id": fmt.Sprint(message["id"]), "error": map[string]any{
					"code":    -32600,
					"message": "Invalid request: missing field `expectedTurnId`",
				}}),
			}, nil
		}
		if expectedTurnID != m.ActiveTurn.ID {
			return [][]byte{
				mustJSON(map[string]any{"id": fmt.Sprint(message["id"]), "error": map[string]any{
					"code":    -32600,
					"message": fmt.Sprintf("expected active turn id `%s` but found `%s`", expectedTurnID, m.ActiveTurn.ID),
				}}),
			}, nil
		}
		inputs, _ := params["input"].([]any)
		text := ""
		for _, input := range inputs {
			object, _ := input.(map[string]any)
			if current, _ := object["text"].(string); current != "" {
				text = current
			}
		}
		m.nextItemID++
		return [][]byte{
			mustJSON(map[string]any{"id": fmt.Sprint(message["id"]), "result": map[string]any{}}),
			mustJSON(map[string]any{"method": "item/started", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"item":     map[string]any{"id": fmt.Sprintf("item-%d", m.nextItemID), "type": "agentMessage"},
			}}),
			mustJSON(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"itemId":   fmt.Sprintf("item-%d", m.nextItemID),
				"delta":    "追加输入：" + strings.TrimSpace(text),
			}}),
			mustJSON(map[string]any{"method": "item/completed", "params": map[string]any{
				"threadId": m.ActiveTurn.ThreadID,
				"turnId":   m.ActiveTurn.ID,
				"item":     map[string]any{"id": fmt.Sprintf("item-%d", m.nextItemID), "type": "agentMessage", "text": "追加输入：" + strings.TrimSpace(text)},
			}}),
		}, nil
	default:
		return nil, nil
	}
}

func (m *MockCodex) CompleteActiveTurn(text string) [][]byte {
	if m.ActiveTurn == nil {
		return nil
	}
	m.nextItemID++
	turn := m.ActiveTurn
	m.ActiveTurn = nil
	return m.completeTurnFrames(turn.ThreadID, turn.ID, fmt.Sprintf("item-%d", m.nextItemID), text)
}

func (m *MockCodex) handleLocalTurn(requestID string, params map[string]any) ([][]byte, error) {
	threadID, _ := params["threadId"].(string)
	m.nextTurnID++
	turnID := fmt.Sprintf("turn-%d", m.nextTurnID)
	m.ActiveTurn = &Turn{ID: turnID, ThreadID: threadID}
	outputs := [][]byte{}
	if requestID != "" && requestID != "<nil>" {
		outputs = append(outputs, mustJSON(map[string]any{"id": requestID, "result": map[string]any{"turn": map[string]any{"id": turnID}}}))
	}
	outputs = append(outputs, mustJSON(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID}}}))
	if m.AutoComplete {
		outputs = append(outputs, m.CompleteActiveTurn("本地操作已完成。")...)
	}
	return outputs, nil
}

func mustJSON(value map[string]any) []byte {
	bytes, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return append(bytes, '\n')
}

func choose(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func (m *MockCodex) resolveRemoteThread(requested string) string {
	switch {
	case requested == "":
		return m.FocusedThreadID
	case m.FocusedThreadID == "", m.FocusedThreadID == requested:
		m.FocusedThreadID = requested
		return requested
	default:
		return m.FocusedThreadID
	}
}

func (m *MockCodex) completeTurnFrames(threadID, turnID, itemID, text string) [][]byte {
	outputs := [][]byte{
		mustJSON(map[string]any{"method": "item/started", "params": map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"item":     map[string]any{"id": itemID, "type": "agentMessage"},
		}}),
	}
	if m.EmitItemDeltas {
		outputs = append(outputs, mustJSON(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"itemId":   itemID,
			"delta":    text,
		}}))
	}
	item := map[string]any{"id": itemID, "type": "agentMessage"}
	if !m.OmitFinalText {
		item["text"] = text
	}
	outputs = append(outputs,
		mustJSON(map[string]any{"method": "item/completed", "params": map[string]any{
			"threadId": threadID,
			"turnId":   turnID,
			"item":     item,
		}}),
		mustJSON(map[string]any{"method": "turn/completed", "params": map[string]any{
			"threadId": threadID,
			"turn":     map[string]any{"id": turnID, "status": "completed", "error": nil},
		}}),
	)
	return outputs
}

func (m *MockCodex) scheduleExit(code int) {
	m.scheduledExit = true
	m.scheduledExitCode = code
}

func (m *MockCodex) ConsumeScheduledExit() (bool, int) {
	if !m.scheduledExit {
		return false, 0
	}
	code := m.scheduledExitCode
	m.scheduledExit = false
	m.scheduledExitCode = 0
	return true, code
}

func stripTrailingTurnCompleted(outputs [][]byte) [][]byte {
	if len(outputs) == 0 {
		return outputs
	}
	last := outputs[len(outputs)-1]
	var message map[string]any
	if err := json.Unmarshal(last, &message); err != nil {
		return outputs
	}
	if method, _ := message["method"].(string); method != "turn/completed" {
		return outputs
	}
	return outputs[:len(outputs)-1]
}
