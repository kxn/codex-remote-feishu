package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

// A real OS child of the production wrapper runs deterministic native protocol
// fixtures. It receives no daemon/store pointers and reports only through the
// actual credentialed loopback CLI dispatcher.
func init() {
	backend := os.Getenv("FSC_NATIVE_PIPELINE_CHILD")
	if backend == "" {
		return
	}
	for _, arg := range os.Args[1:] {
		if arg == "--version" {
			fmt.Println(backend + " 0.169.1")
			os.Exit(0)
		}
	}
	if err := runPipelineNativeChild(backend); err != nil {
		fmt.Fprintln(os.Stderr, "native pipeline child:", err)
		os.Exit(2)
	}
	os.Exit(0)
}

func pipelineEmit(value any) { raw, _ := json.Marshal(value); fmt.Println(string(raw)) }

func runPipelineNativeChild(backend string) error {
	owner := map[string]any{}
	_ = json.Unmarshal([]byte(os.Getenv(config.WorkOwnerBindingEnv)), &owner)
	sessionID := "pipeline-" + backend + "-session-" + fmt.Sprint(owner["workerSessionId"])
	turnCounter := 0
	claudeUserCount := 0
	var heldNativeFrame map[string]any
	turnID := ""
	cwd, _ := os.Getwd()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var frame map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return err
		}
		if backend == "claude" {
			if frame["type"] == "control_request" {
				pipelineEmit(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": frame["request_id"], "response": map[string]any{"models": []any{map[string]any{"value": "default", "displayName": "fixture"}}}}})
				continue
			}
			if frame["type"] != "user" {
				continue
			}
			pipelineEmit(map[string]any{"type": "system", "subtype": "init", "session_id": sessionID, "cwd": cwd, "permissionMode": "default"})
			pipelineEmit(map[string]any{"type": "user", "uuid": frame["uuid"], "session_id": sessionID, "message": frame["message"]})
			if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") != "true" || claudeUserCount == 0 {
				pipelineEmit(map[string]any{"type": "assistant", "session_id": sessionID, "message": map[string]any{"id": "pipeline-progress", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Starting the explicitly admitted work."}}}})
			}
			if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
				claudeUserCount++
				if claudeUserCount == 1 {
					continue
				}
				if e := pipelineVerifyMediaFrame(frame); e != nil {
					return e
				}
			}
			if err := pipelineChildBusiness(frame); err != nil {
				return err
			}
			pipelineEmit(map[string]any{"type": "assistant", "session_id": sessionID, "message": map[string]any{"id": "pipeline-final", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "pipeline native final"}}}})
			pipelineEmit(map[string]any{"type": "result", "subtype": "success", "session_id": sessionID, "result": "pipeline native final", "is_error": false})
			continue
		}
		id := frame["id"]
		params, _ := frame["params"].(map[string]any)
		switch frame["method"] {
		case "initialize":
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"userAgent": "controlled-native-fixture"}})
		case "thread/start", "thread/resume":
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": sessionID, "cwd": params["cwd"], "turns": []any{}}}})
		case "turn/start":
			turnCounter++
			turnID = fmt.Sprintf("pipeline-native-turn-%d", turnCounter)
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}}})
			pipelineEmit(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": sessionID, "turn": map[string]any{"id": turnID, "status": "inProgress"}}})
			if os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") == "true" {
				heldNativeFrame = frame
				continue
			}
			if err := pipelineChildBusiness(frame); err != nil {
				return err
			}
			pipelineEmit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": sessionID, "turnId": turnID, "item": map[string]any{"id": "pipeline-final", "type": "agentMessage", "text": "pipeline native final"}}})
			pipelineEmit(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": sessionID, "turn": map[string]any{"id": turnID, "status": "completed"}}})
		case "thread/shellCommand":
			if err := pipelineNativePrivateShell(frame, sessionID); err != nil {
				return err
			}
			if err := pipelineChildBusiness(heldNativeFrame); err != nil {
				return err
			}
			pipelineEmit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": sessionID, "turnId": turnID, "item": map[string]any{"id": "pipeline-final", "type": "agentMessage", "text": "pipeline native final"}}})
			pipelineEmit(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": sessionID, "turn": map[string]any{"id": turnID, "status": "completed"}}})

		case "turn/steer":
			if e := pipelineVerifyMediaFrame(frame); e != nil {
				return e
			}
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"turnId": turnID}})
			if e := pipelineChildBusiness(frame); e != nil {
				return e
			}
			pipelineEmit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": sessionID, "turnId": turnID, "item": map[string]any{"id": "pipeline-final", "type": "agentMessage", "text": "pipeline native final"}}})
			pipelineEmit(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": sessionID, "turn": map[string]any{"id": turnID, "status": "completed"}}})

		case "thread/read":
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": sessionID, "status": map[string]any{"type": "idle"}, "turns": []any{map[string]any{"id": turnID, "status": "completed", "items": []any{}}}}}})
		case "thread/goal/get":
			pipelineEmit(map[string]any{"id": id, "result": map[string]any{"goal": nil}})
		case "initialized":
		default:
			if id != nil {
				pipelineEmit(map[string]any{"id": id, "result": map[string]any{}})
			}
		}
	}
	return scanner.Err()
}

type pipelineWorkEnvelope struct {
	RunRef string                       `json:"run_ref"`
	Task   *workservice.FrozenTaskInput `json:"task,omitempty"`
	Inputs []pipelineWorkInput          `json:"inputs"`
}

type pipelineWorkInput struct {
	ID          string        `json:"id"`
	EventID     string        `json:"event_id"`
	Body        string        `json:"body"`
	RelatedTask *work.Task    `json:"related_task_read_only,omitempty"`
	Message     *work.Message `json:"message,omitempty"`
}

func pipelineFindEnvelope(value any) (pipelineWorkEnvelope, bool) {
	if text, ok := value.(string); ok {
		originalText := text
		var envelope pipelineWorkEnvelope
		if start := strings.Index(text, "\n{"); start >= 0 {
			text = text[start+1:]
		}
		if json.NewDecoder(strings.NewReader(text)).Decode(&envelope) == nil && envelope.RunRef != "" && len(envelope.Inputs) > 0 {
			return envelope, true
		}
		// Personal prompts carry readable original text and separately labelled
		// durable source IDs instead of the team JSON envelope.
		if start := strings.Index(originalText, "使用宿主能力时的执行引用：--run-ref "); start >= 0 {
			privateText := originalText[start:]
			first, _, _ := strings.Cut(privateText, "\n")
			envelope.RunRef, _, _ = strings.Cut(strings.TrimPrefix(first, "使用宿主能力时的执行引用：--run-ref "), "。")
			header, _, hasBody := strings.Cut(privateText, "\n\n用户消息：\n")
			for _, line := range strings.Split(header, "\n") {
				if !strings.HasPrefix(line, "输入来源：") {
					continue
				}
				var inputID, messageID, platformID string
				if n, _ := fmt.Sscanf(line, "输入来源：input_id=%q message_id=%q platform_message_id=%q", &inputID, &messageID, &platformID); n != 3 || inputID == "" || messageID == "" {
					return pipelineWorkEnvelope{}, false
				}
				message := &work.Message{ID: messageID, Source: work.Actor{Kind: work.ActorPrincipal}}
				if platformID != "" {
					message.PlatformKey = &work.MessageKey{MessageID: platformID}
				}
				envelope.Inputs = append(envelope.Inputs, pipelineWorkInput{ID: inputID, Message: message, Body: privateText})
			}
			if envelope.RunRef != "" && len(envelope.Inputs) > 0 && hasBody {
				return envelope, true
			}
		}
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if result, ok := pipelineFindEnvelope(child); ok {
				return result, true
			}
		}
	case []any:
		for _, child := range typed {
			if result, ok := pipelineFindEnvelope(child); ok {
				return result, true
			}
		}
	}
	return pipelineWorkEnvelope{}, false
}

func pipelineChildBusiness(frame map[string]any) error {
	if os.Getenv("FSC_NATIVE_PIPELINE_IMAGES") == "true" {
		if e := pipelineVerifyMediaFrame(frame); e != nil {
			return e
		}
	}
	envelope, ok := pipelineFindEnvelope(frame)
	if !ok {
		return fmt.Errorf("native prompt lacks admitted work sources")
	}
	if os.Getenv("FSC_NATIVE_PIPELINE_AUTO_PRIVATE") == "true" && os.Getenv("FSC_NATIVE_PIPELINE_IMAGES") != "true" && os.Getenv("FSC_NATIVE_PIPELINE_CLAUDE_STEER") != "true" {
		first := envelope.Inputs[0]
		if first.ID == "" || first.Message == nil || first.Message.ID == "" || first.Message.PlatformKey == nil || first.Message.PlatformKey.MessageID != "sdk-human-message" || !strings.Contains(first.Body, "Complete my authorized work and reply here.") {
			return fmt.Errorf("native private prompt lost actual SDK source or readable text")
		}
	}

	help, err := pipelineTeamCLI(nil, "help", "outcome.report")
	if err != nil {
		return err
	}
	if !bytes.Contains(help, []byte("outcome.report")) {
		return fmt.Errorf("actual team CLI help did not expose report contract")
	}
	execute := func(name string, payload, result any) error {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		output, err := pipelineTeamCLI(body, name, "--run-ref", envelope.RunRef, "--stdin")
		if err == nil && result != nil {
			err = json.Unmarshal(output, result)
		}
		return err
	}
	call := func(name string, payload any) error { return execute(name, payload, nil) }

	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := call("context", map[string]any{}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("actual native turn never became authorized running work")
		}
		time.Sleep(5 * time.Millisecond)
	}
	disposition := "complete"
	if os.Getenv("FSC_NATIVE_PIPELINE_COLLABORATION") != "" {
		disposition, err = pipelineCollaborate(envelope, execute)
		if err != nil {
			return err
		}
	} else if os.Getenv("FSC_NATIVE_PIPELINE_DEPENDENCY") != "" {
		disposition, err = pipelineDependency(envelope, execute)
		if err != nil {
			return err
		}
	} else if os.Getenv("FSC_NATIVE_PIPELINE_GROUP") != "" {
		owner := map[string]any{}
		_ = json.Unmarshal([]byte(os.Getenv(config.WorkOwnerBindingEnv)), &owner)
		role, _ := owner["roleId"].(string)
		human := false
		for _, input := range envelope.Inputs {
			if input.Message != nil && input.Message.Source.Kind == "principal" {
				human = true
			}
		}
		if role == "pipeline-expert" {
			if err := call("message.send", map[string]any{"key": "expert-reply", "recipient_role_id": "pipeline-role", "conversation_id": "pipeline-handoff", "body": "Expert completed the requested analysis; return this result to the human."}); err != nil {
				return err
			}
		} else if human {
			if err := call("task.assign", map[string]any{"key": "delegate-to-expert", "title": "Analyze the authorized request and reply to the leader", "owner_role_id": "pipeline-expert"}); err != nil {
				return err
			}
			disposition = "unfinished"
		} else if err := call("message.publish", map[string]any{"key": "leader-group-reply", "body": "Leader received the internal expert result and replies to the source group.", "purpose": "final"}); err != nil {
			return err
		}
		if os.Getenv("FSC_NATIVE_PIPELINE_GROUP") == "2" && (role == "pipeline-expert" || human) {
			deadline := time.Now().Add(8 * time.Second)
			for {
				response, e := http.Get(strings.TrimRight(os.Getenv(config.WorkToolEndpointEnv), "/") + "/pipeline/overlap")
				if e == nil {
					io.Copy(io.Discard, response.Body)
					response.Body.Close()
					if response.StatusCode == http.StatusOK {
						break
					}
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("independent native turns never overlapped")
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	} else if os.Getenv("FSC_NATIVE_PIPELINE_AUTO_PRIVATE") != "true" {
		if err := call("message.publish", map[string]any{"key": "pipeline-reply", "body": "pipeline explicit source reply", "reply_to_input_id": envelope.Inputs[0].ID}); err != nil {
			return err
		}
	}
	// Ordinary private replies must settle without a model-side team report.
	// Group work still exercises the explicit task/report contract below.
	if os.Getenv("FSC_NATIVE_PIPELINE_AUTO_PRIVATE") == "true" {
		return nil
	}
	ids := []string{}
	for _, input := range envelope.Inputs {
		ids = append(ids, input.ID)
	}
	return call("outcome.report", map[string]any{"key": "pipeline-outcome:" + envelope.RunRef, "expected_revision": 0, "processed_input_ids": ids, "summary": "pipeline explicit business report", "checkpoint": "pipeline checkpoint", "work_disposition": disposition})
}

func pipelineTeamCLI(body []byte, args ...string) ([]byte, error) {
	binary := os.Getenv(config.WorkTeamBinaryEnv)
	if !filepath.IsAbs(binary) {
		return nil, fmt.Errorf("native process did not receive an absolute team CLI path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := execlaunch.CommandContext(ctx, binary, append([]string{"team"}, args...)...)
	cmd.Stdin = bytes.NewReader(body)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("team %s: %w: %s", args[0], err, output)
	}
	return output, nil
}
