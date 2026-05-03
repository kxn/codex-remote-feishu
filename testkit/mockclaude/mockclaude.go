package mockclaude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type Scenario string

const (
	ScenarioHello             Scenario = "hello"
	ScenarioToolApproval      Scenario = "tool-approval"
	ScenarioAskUserQuestion   Scenario = "ask-user-question"
	ScenarioPlanConfirmation  Scenario = "plan-confirmation"
	ScenarioTextSteer         Scenario = "text-steer"
	ScenarioInterrupt         Scenario = "interrupt"
	ScenarioExitWithoutResult Scenario = "exit-without-result"
)

type MockClaude struct {
	Scenario       Scenario
	SessionID      string
	CWD            string
	Model          string
	PermissionMode string

	nextMessageID int
	nextRequestID int
	nextToolUseID int

	pendingApproval    *pendingApproval
	interruptReady     bool
	textSteerArmed     bool
	exitAfterWriteCode int
}

type pendingApproval struct {
	Scenario  Scenario
	RequestID string
	ToolUseID string
	ToolName  string
	Input     map[string]any
}

type ExitCodeError struct {
	Code int
}

func (e ExitCodeError) Error() string {
	return fmt.Sprintf("mockclaude exit code %d", e.Code)
}

func NewFromEnv() *MockClaude {
	return New(os.Getenv("MOCKCLAUDE_SCENARIO"))
}

func NewFromEnvAndArgs(args []string) *MockClaude {
	mock := NewFromEnv()
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		mock.CWD = strings.TrimSpace(cwd)
	}
	for index := 0; index < len(args); index++ {
		switch strings.TrimSpace(args[index]) {
		case "--resume":
			if index+1 < len(args) {
				index++
				mock.SessionID = strings.TrimSpace(args[index])
			}
		}
	}
	return mock
}

func New(rawScenario string) *MockClaude {
	scenario := Scenario(strings.TrimSpace(rawScenario))
	if scenario == "" {
		scenario = ScenarioHello
	}
	permissionMode := "default"
	if scenario == ScenarioPlanConfirmation {
		permissionMode = "plan"
	}
	return &MockClaude{
		Scenario:       scenario,
		SessionID:      "mock-claude-session-1",
		CWD:            "/data/dl/droid",
		Model:          "mimo-v2.5-pro",
		PermissionMode: permissionMode,
	}
}

func RunIO(mock *MockClaude, stdin io.Reader, stdout io.Writer) error {
	if mock == nil {
		mock = New("")
	}
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		frames, err := mock.HandleLine(scanner.Bytes())
		if err != nil {
			return err
		}
		for _, frame := range frames {
			if _, err := stdout.Write(frame); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
		}
		if code := mock.consumeExitAfterWriteCode(); code != 0 {
			return ExitCodeError{Code: code}
		}
	}
	return scanner.Err()
}

func (m *MockClaude) HandleLine(raw []byte) ([][]byte, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	switch strings.TrimSpace(stringValue(message["type"])) {
	case "control_request":
		return m.handleControlRequest(message)
	case "control_response":
		return m.handleControlResponse(message)
	case "user":
		return m.handleUserMessage(message)
	default:
		return nil, nil
	}
}

func (m *MockClaude) handleControlRequest(message map[string]any) ([][]byte, error) {
	requestID := strings.TrimSpace(stringValue(message["request_id"]))
	request := mapValue(message["request"])
	switch strings.TrimSpace(stringValue(request["subtype"])) {
	case "initialize":
		return [][]byte{
			mustLine(map[string]any{
				"type": "control_response",
				"response": map[string]any{
					"subtype":    "success",
					"request_id": requestID,
					"response": map[string]any{
						"commands": []any{},
					},
				},
			}),
			mustLine(map[string]any{
				"type":           "system",
				"subtype":        "init",
				"cwd":            m.CWD,
				"session_id":     m.SessionID,
				"tools":          toolsForScenario(m.Scenario),
				"model":          m.Model,
				"permissionMode": m.PermissionMode,
			}),
		}, nil
	case "set_permission_mode":
		if mode := strings.TrimSpace(stringValue(request["mode"])); mode != "" {
			m.PermissionMode = mode
		}
		return [][]byte{
			mustLine(map[string]any{
				"type": "control_response",
				"response": map[string]any{
					"subtype":    "success",
					"request_id": requestID,
				},
			}),
			mustLine(map[string]any{
				"type":           "system",
				"subtype":        "status",
				"permissionMode": m.PermissionMode,
			}),
		}, nil
	case "interrupt":
		if !m.interruptReady {
			return [][]byte{m.controlSuccess(requestID, nil)}, nil
		}
		m.interruptReady = false
		return [][]byte{
			m.controlSuccess(requestID, nil),
			m.userInterruptFrame("[Request interrupted by user]"),
			m.resultFrame("error_during_execution", "", false),
		}, nil
	default:
		return [][]byte{m.controlSuccess(requestID, nil)}, nil
	}
}

func (m *MockClaude) handleControlResponse(message map[string]any) ([][]byte, error) {
	response := mapValue(message["response"])
	requestID := strings.TrimSpace(firstNonEmpty(
		stringValue(message["request_id"]),
		stringValue(response["request_id"]),
	))
	if m.pendingApproval == nil || requestID == "" || requestID != m.pendingApproval.RequestID {
		return nil, nil
	}
	pending := m.pendingApproval
	m.pendingApproval = nil
	body := mapValue(response["response"])
	behavior := strings.TrimSpace(stringValue(body["behavior"]))
	switch pending.Scenario {
	case ScenarioToolApproval:
		return m.finishToolApproval(pending, body, behavior), nil
	case ScenarioAskUserQuestion:
		return m.finishAskUserQuestion(pending, body, behavior), nil
	case ScenarioPlanConfirmation:
		return m.finishPlanConfirmation(pending, body, behavior), nil
	default:
		return nil, nil
	}
}

func (m *MockClaude) handleUserMessage(message map[string]any) ([][]byte, error) {
	content := mapValue(message["message"])["content"]
	if strings.TrimSpace(stringValue(content)) == "" {
		return nil, nil
	}
	switch m.Scenario {
	case ScenarioToolApproval:
		return m.startToolApproval(), nil
	case ScenarioAskUserQuestion:
		return m.startAskUserQuestion(), nil
	case ScenarioPlanConfirmation:
		return m.startPlanConfirmation(), nil
	case ScenarioTextSteer:
		if !m.textSteerArmed {
			m.textSteerArmed = true
			return m.startStreamingOnly(), nil
		}
		finalText := "Steer merged: " + strings.TrimSpace(stringValue(content))
		return m.startPlainText(finalText), nil
	case ScenarioInterrupt:
		m.interruptReady = true
		return m.startStreamingOnly(), nil
	case ScenarioExitWithoutResult:
		m.exitAfterWriteCode = 143
		return m.textMessageFrames("Partial output before process exit."), nil
	default:
		return m.startPlainText("Claude mock ready."), nil
	}
}

func (m *MockClaude) consumeExitAfterWriteCode() int {
	if m == nil {
		return 0
	}
	code := m.exitAfterWriteCode
	m.exitAfterWriteCode = 0
	return code
}

func (m *MockClaude) startPlainText(text string) [][]byte {
	frames := m.textMessageFrames(text)
	return append(frames, m.resultFrame("success", text, false))
}

func (m *MockClaude) startStreamingOnly() [][]byte {
	return [][]byte{m.messageStartFrame()}
}

func (m *MockClaude) startToolApproval() [][]byte {
	input := map[string]any{
		"command":     "printf BLACKBOX_TOOL_OK",
		"description": "Print the string BLACKBOX_TOOL_OK",
	}
	return m.startToolScenario(ScenarioToolApproval, "Bash", input, "")
}

func (m *MockClaude) startAskUserQuestion() [][]byte {
	input := map[string]any{
		"questions": []any{
			map[string]any{
				"question":    "Which approach should I take?",
				"header":      "Approach",
				"multiSelect": false,
				"options": []any{
					map[string]any{"label": "Fast", "description": "Prioritize speed"},
					map[string]any{"label": "Safe", "description": "Prioritize safety"},
					map[string]any{"label": "Balanced", "description": "Balance both"},
				},
			},
		},
	}
	return m.startToolScenario(ScenarioAskUserQuestion, "AskUserQuestion", input, "")
}

func (m *MockClaude) startPlanConfirmation() [][]byte {
	input := map[string]any{}
	return m.startToolScenario(ScenarioPlanConfirmation, "ExitPlanMode", input, "1. Update README.txt line 1.\n2. Save the file without further edits.")
}

func (m *MockClaude) startToolScenario(scenario Scenario, toolName string, input map[string]any, leadingText string) [][]byte {
	requestID := m.nextID("req", &m.nextRequestID)
	toolUseID := m.nextID("call", &m.nextToolUseID)
	messageID := m.nextID("msg", &m.nextMessageID)
	frames := [][]byte{m.messageStartFrameWithID(messageID)}
	if strings.TrimSpace(leadingText) != "" {
		frames = append(frames, m.assistantTextFrame(messageID, leadingText))
	}
	frames = append(frames, m.assistantToolUseFrame(messageID, toolUseID, toolName, input))
	frames = append(frames, mustLine(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   toolName,
			"input":       input,
			"tool_use_id": toolUseID,
		},
	}))
	m.pendingApproval = &pendingApproval{
		Scenario:  scenario,
		RequestID: requestID,
		ToolUseID: toolUseID,
		ToolName:  toolName,
		Input:     cloneMap(input),
	}
	return frames
}

func (m *MockClaude) finishToolApproval(pending *pendingApproval, body map[string]any, behavior string) [][]byte {
	if behavior != "allow" {
		return [][]byte{
			m.userToolResultFrame(pending.ToolUseID, firstNonEmpty(stringValue(body["message"]), "tool request denied"), true, "Error: tool request denied"),
			m.resultFrame("error_during_execution", "", false),
		}
	}
	finalText := "BLACKBOX_TOOL_OK"
	frames := [][]byte{
		m.userToolResultFrame(pending.ToolUseID, finalText, false, map[string]any{
			"stdout":      finalText,
			"stderr":      "",
			"interrupted": false,
			"isImage":     false,
		}),
	}
	frames = append(frames, m.textMessageFrames(finalText)...)
	return append(frames, m.resultFrame("success", finalText, false))
}

func (m *MockClaude) finishAskUserQuestion(pending *pendingApproval, body map[string]any, behavior string) [][]byte {
	if behavior != "allow" {
		return [][]byte{
			m.userToolResultFrame(pending.ToolUseID, "question request denied", true, "Error: question request denied"),
			m.resultFrame("error_during_execution", "", false),
		}
	}
	updatedInput := mapValue(body["updatedInput"])
	answers := mapValue(updatedInput["answers"])
	answer := firstAnswerValue(answers)
	frames := [][]byte{
		m.userToolResultFrame(pending.ToolUseID, "User answered the pending question.", false, map[string]any{
			"questions": cloneJSONValue(updatedInput["questions"]),
			"answers":   cloneMap(answers),
		}),
	}
	finalText := "Got it — I'll use the " + firstNonEmpty(answer, "Fast") + " approach."
	frames = append(frames, m.textMessageFrames(finalText)...)
	return append(frames, m.resultFrame("success", finalText, false))
}

func (m *MockClaude) finishPlanConfirmation(pending *pendingApproval, body map[string]any, behavior string) [][]byte {
	if behavior != "allow" {
		message := firstNonEmpty(stringValue(body["message"]), "plan rejected")
		return [][]byte{
			m.userToolResultFrame(pending.ToolUseID, message, true, "Error: "+message),
			m.userInterruptFrame("[Request interrupted by user for tool use]"),
			m.resultFrame("error_during_execution", "", false),
		}
	}
	updatedInput := mapValue(body["updatedInput"])
	feedback := firstNonEmpty(stringValue(updatedInput["feedback"]), "Approved. Execute the plan.")
	frames := [][]byte{
		m.userToolResultFrame(pending.ToolUseID, "Plan approved", false, map[string]any{
			"feedback": feedback,
			"filePath": "/tmp/mock-claude-plan.md",
		}),
	}
	finalText := "Plan approved. Proceeding with the requested change."
	frames = append(frames, m.textMessageFrames(finalText)...)
	return append(frames, m.resultFrame("success", finalText, false))
}

func (m *MockClaude) messageStartFrame() []byte {
	return m.messageStartFrameWithID(m.nextID("msg", &m.nextMessageID))
}

func (m *MockClaude) messageStartFrameWithID(messageID string) []byte {
	return mustLine(map[string]any{
		"type": "stream_event",
		"event": map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      messageID,
				"type":    "message",
				"role":    "assistant",
				"model":   m.Model,
				"content": []any{},
				"usage":   map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		},
		"session_id": m.SessionID,
	})
}

func (m *MockClaude) textMessageFrames(text string) [][]byte {
	messageID := m.nextID("msg", &m.nextMessageID)
	return [][]byte{
		m.messageStartFrameWithID(messageID),
		m.assistantTextFrame(messageID, text),
	}
}

func (m *MockClaude) assistantTextFrame(messageID, text string) []byte {
	return mustLine(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    messageID,
			"type":  "message",
			"role":  "assistant",
			"model": m.Model,
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
		"session_id": m.SessionID,
	})
}

func (m *MockClaude) assistantToolUseFrame(messageID, toolUseID, toolName string, input map[string]any) []byte {
	return mustLine(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    messageID,
			"type":  "message",
			"role":  "assistant",
			"model": m.Model,
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    toolUseID,
					"name":  toolName,
					"input": cloneMap(input),
				},
			},
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
		"session_id": m.SessionID,
	})
}

func (m *MockClaude) userToolResultFrame(toolUseID, content string, isError bool, toolUseResult any) []byte {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": toolUseID,
					"content":     content,
					"is_error":    isError,
				},
			},
		},
		"session_id":      m.SessionID,
		"tool_use_result": cloneJSONValue(toolUseResult),
	}
	return mustLine(payload)
}

func (m *MockClaude) userInterruptFrame(text string) []byte {
	return mustLine(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "text",
					"text": text,
				},
			},
		},
		"session_id": m.SessionID,
	})
}

func (m *MockClaude) resultFrame(subtype, result string, isError bool) []byte {
	payload := map[string]any{
		"type":       "result",
		"subtype":    subtype,
		"is_error":   isError,
		"session_id": m.SessionID,
		"usage": map[string]any{
			"input_tokens":                1,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
			"output_tokens":               len([]rune(result)),
		},
		"modelUsage": map[string]any{
			m.Model: map[string]any{
				"inputTokens":              1,
				"outputTokens":             len([]rune(result)),
				"cacheReadInputTokens":     0,
				"cacheCreationInputTokens": 0,
				"contextWindow":            200000,
			},
		},
		"errors": []any{},
	}
	if strings.TrimSpace(result) != "" {
		payload["result"] = result
	}
	return mustLine(payload)
}

func (m *MockClaude) controlSuccess(requestID string, body any) []byte {
	response := map[string]any{
		"subtype":    "success",
		"request_id": requestID,
	}
	if body != nil {
		response["response"] = body
	}
	return mustLine(map[string]any{
		"type":     "control_response",
		"response": response,
	})
}

func (m *MockClaude) nextID(prefix string, counter *int) string {
	*counter = *counter + 1
	return fmt.Sprintf("%s_%d", strings.TrimSpace(prefix), *counter)
}

func toolsForScenario(scenario Scenario) []string {
	switch scenario {
	case ScenarioToolApproval, ScenarioInterrupt:
		return []string{"Bash"}
	case ScenarioAskUserQuestion:
		return []string{"AskUserQuestion"}
	case ScenarioPlanConfirmation:
		return []string{"ExitPlanMode", "AskUserQuestion", "EnterPlanMode"}
	case ScenarioExitWithoutResult:
		return nil
	default:
		return []string{"Bash"}
	}
}

func mustLine(payload any) []byte {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return append(raw, '\n')
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneJSONValue(item))
		}
		return out
	default:
		return value
	}
}

func mapValue(value any) map[string]any {
	current, _ := value.(map[string]any)
	if current == nil {
		return map[string]any{}
	}
	return current
}

func stringValue(value any) string {
	current, _ := value.(string)
	return current
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstAnswerValue(answers map[string]any) string {
	for _, value := range answers {
		return strings.TrimSpace(stringValue(value))
	}
	return ""
}
