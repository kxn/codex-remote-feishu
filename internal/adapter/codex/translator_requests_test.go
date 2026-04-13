package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerRequestStartedProducesApprovalEvent(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`)); err != nil {
		t.Fatalf("observe turn started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"serverRequest/started","params":{"thread":{"id":"thread-1"},"turn":{"id":"turn-1"},"request":{"id":"req-1","type":"approval","title":"Run command?","message":"Need approval before continuing.","command":"git push","acceptLabel":"Allow","declineLabel":"Block"}}}`))
	if err != nil {
		t.Fatalf("observe request started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one request started event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.RequestID != "req-1" {
		t.Fatalf("unexpected request started event: %#v", event)
	}
	if event.Initiator.Kind != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected remote initiator, got %#v", event)
	}
	if event.Metadata["requestType"] != "approval" || event.Metadata["title"] != "Run command?" {
		t.Fatalf("unexpected request metadata: %#v", event.Metadata)
	}
	body, _ := event.Metadata["body"].(string)
	if !strings.Contains(body, "Need approval before continuing.") || !strings.Contains(body, "git push") {
		t.Fatalf("expected message and command in body, got %#v", event.Metadata)
	}
	if event.Metadata["acceptLabel"] != "Allow" || event.Metadata["declineLabel"] != "Block" {
		t.Fatalf("unexpected approval labels: %#v", event.Metadata)
	}
}

func TestObserveServerRequestStartedNormalizesApprovalKindAndExtractsOptions(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"serverRequest/started","params":{"thread":{"id":"thread-1"},"turn":{"id":"turn-1"},"request":{"id":"req-2","type":"approval_command","title":"Run command?","options":[{"id":"accept","label":"Allow"},{"id":"acceptForSession","label":"Allow this session"},{"id":"decline","label":"Decline"}]}}}`))
	if err != nil {
		t.Fatalf("observe request started: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one request started event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Metadata["requestType"] != "approval" || event.Metadata["requestKind"] != "approval_command" {
		t.Fatalf("unexpected normalized request metadata: %#v", event.Metadata)
	}
	options, ok := event.Metadata["options"].([]map[string]any)
	if !ok || len(options) != 3 {
		t.Fatalf("expected extracted options, got %#v", event.Metadata["options"])
	}
	if options[1]["id"] != "acceptForSession" {
		t.Fatalf("unexpected extracted options payload: %#v", options)
	}
}

func TestObserveServerRequestUserInputProducesQuestionMetadata(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"id":"req-ui-1","method":"item/tool/requestUserInput","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","questions":[{"id":"model","header":"模型","question":"请选择模型","options":[{"label":"gpt-5.4","description":"推荐"},{"label":"gpt-5.3"}]},{"id":"notes","header":"备注","question":"补充说明","isOther":true,"isSecret":true}]}}`))
	if err != nil {
		t.Fatalf("observe request user input: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one request started event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.RequestID != "req-ui-1" {
		t.Fatalf("unexpected request event: %#v", event)
	}
	if event.Metadata["requestType"] != "request_user_input" || event.Metadata["itemId"] != "item-1" {
		t.Fatalf("unexpected request user input metadata: %#v", event.Metadata)
	}
	questions, ok := event.Metadata["questions"].([]map[string]any)
	if !ok || len(questions) != 2 {
		t.Fatalf("expected request questions metadata, got %#v", event.Metadata["questions"])
	}
	if questions[0]["id"] != "model" || questions[1]["isSecret"] != true {
		t.Fatalf("unexpected request question payload: %#v", questions)
	}
}

func TestObserveServerRequestResolvedSupportsLegacyMethod(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("observe client thread resume: %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		Kind:   agentproto.CommandPromptSend,
		Origin: agentproto.Origin{Surface: "surface-1"},
		Target: agentproto.Target{ThreadID: "thread-1", CWD: "/tmp/project"},
		Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}}},
	}); err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turn":{"id":"turn-1"}}}`)); err != nil {
		t.Fatalf("observe turn started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"request/resolved","params":{"threadId":"thread-1","turnId":"turn-1","requestId":"req-1","result":{"decision":"decline"}}}`))
	if err != nil {
		t.Fatalf("observe request resolved: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one request resolved event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventRequestResolved || event.RequestID != "req-1" {
		t.Fatalf("unexpected request resolved event: %#v", event)
	}
	if event.Metadata["decision"] != "decline" {
		t.Fatalf("unexpected resolved request metadata: %#v", event.Metadata)
	}
}

func TestTranslateRequestRespondApproval(t *testing.T) {
	tr := NewTranslator("inst-1")
	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-1",
			Response: map[string]any{
				"type":     "approval",
				"decision": "acceptForSession",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate request respond: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("unmarshal request respond payload: %v", err)
	}
	result, _ := payload["result"].(map[string]any)
	if payload["id"] != "req-1" || result["decision"] != "acceptForSession" {
		t.Fatalf("unexpected request respond payload: %#v", payload)
	}
}

func TestTranslateRequestRespondApprovalFallsBackToApprovedBool(t *testing.T) {
	tr := NewTranslator("inst-1")
	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-legacy",
			Response: map[string]any{
				"type":     "approval",
				"approved": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("translate request respond: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("unmarshal request respond payload: %v", err)
	}
	result, _ := payload["result"].(map[string]any)
	if payload["id"] != "req-legacy" || result["decision"] != "accept" {
		t.Fatalf("unexpected legacy request respond payload: %#v", payload)
	}
}

func TestTranslateRequestRespondUserInputPreservesAnswerPayload(t *testing.T) {
	tr := NewTranslator("inst-1")
	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-ui-1",
			Response: map[string]any{
				"answers": map[string]any{
					"model": map[string]any{"answers": []string{"gpt-5.4"}},
					"notes": map[string]any{"answers": []string{"请用中文回复"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate request respond: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("unmarshal request respond payload: %v", err)
	}
	result, _ := payload["result"].(map[string]any)
	answers, _ := result["answers"].(map[string]any)
	if payload["id"] != "req-ui-1" || len(answers) != 2 {
		t.Fatalf("unexpected request user input response payload: %#v", payload)
	}
}

func TestObserveServerItemLifecycleAndDelta(t *testing.T) {
	tr := NewTranslator("inst-1")

	started, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage"}}}`))
	if err != nil {
		t.Fatalf("observe item started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one item started event, got %#v", started.Events)
	}
	if started.Events[0].Kind != agentproto.EventItemStarted || started.Events[0].ItemKind != "agent_message" {
		t.Fatalf("unexpected item started event: %#v", started.Events[0])
	}

	delta, err := tr.ObserveServer([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"您好"}}`))
	if err != nil {
		t.Fatalf("observe item delta: %v", err)
	}
	if len(delta.Events) != 1 {
		t.Fatalf("expected one item delta event, got %#v", delta.Events)
	}
	if delta.Events[0].Kind != agentproto.EventItemDelta || delta.Events[0].Delta != "您好" {
		t.Fatalf("unexpected item delta event: %#v", delta.Events[0])
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"agentMessage"}}}`))
	if err != nil {
		t.Fatalf("observe item completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one item completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Kind != agentproto.EventItemCompleted || completed.Events[0].ItemKind != "agent_message" {
		t.Fatalf("unexpected item completed event: %#v", completed.Events[0])
	}
}

func TestObserveServerDynamicToolCallCompletedExtractsContentItems(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"tool-1","type":"dynamicToolCall","tool":"demo_tool","contentItems":[{"type":"inputText","text":"dynamic-ok"},{"type":"inputImage","imageUrl":"data:image/png;base64,AAA"}]}}}`))
	if err != nil {
		t.Fatalf("observe dynamic tool item completed: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "dynamic_tool_call" {
		t.Fatalf("unexpected item completed event: %#v", event)
	}
	if event.Metadata["tool"] != "demo_tool" || event.Metadata["text"] != "dynamic-ok" {
		t.Fatalf("unexpected dynamic tool metadata: %#v", event.Metadata)
	}
	contentItems, ok := event.Metadata["contentItems"].([]map[string]any)
	if !ok || len(contentItems) != 2 {
		t.Fatalf("expected structured content items, got %#v", event.Metadata["contentItems"])
	}
	if contentItems[1]["type"] != "image" || contentItems[1]["imageBase64"] != "data:image/png;base64,AAA" {
		t.Fatalf("unexpected dynamic tool image payload: %#v", contentItems)
	}
}

func TestObserveServerDynamicToolCallStructuredOutputFallsBackToSummaryText(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"tool-2","type":"dynamicToolCall","tool":"demo_tool","output":{"status":"ok","count":2}}}}`))
	if err != nil {
		t.Fatalf("observe dynamic tool structured output: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventItemCompleted || event.ItemKind != "dynamic_tool_call" {
		t.Fatalf("unexpected item completed event: %#v", event)
	}
	if event.Metadata["text"] != `{"count":2,"status":"ok"}` {
		t.Fatalf("expected compact structured summary, got %#v", event.Metadata)
	}
}

func TestObserveServerCompletedLegacyAssistantMessageMapsToAgentMessage(t *testing.T) {
	tr := NewTranslator("inst-1")
	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"item-1","type":"assistant_message","text":"hello"}}}`))
	if err != nil {
		t.Fatalf("observe item completed: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	if result.Events[0].ItemKind != "agent_message" {
		t.Fatalf("expected normalized agent_message kind, got %#v", result.Events[0])
	}
	text, _ := result.Events[0].Metadata["text"].(string)
	if text != "hello" {
		t.Fatalf("expected completed text to be preserved, got %#v", result.Events[0].Metadata)
	}
}

func TestObserveServerFileChangeLifecyclePreservesStructuredChanges(t *testing.T) {
	tr := NewTranslator("inst-1")

	started, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"file-1","type":"fileChange","status":"inProgress","changes":[{"path":"old.txt","kind":{"type":"update","move_path":"new.txt"},"diff":"@@ -1 +1 @@\n-old\n+new"},{"path":"added.txt","kind":{"type":"add"},"diff":"line 1\nline 2"}]}}}`))
	if err != nil {
		t.Fatalf("observe file change started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one file change started event, got %#v", started.Events)
	}
	startedEvent := started.Events[0]
	if startedEvent.Kind != agentproto.EventItemStarted || startedEvent.ItemKind != "file_change" || startedEvent.Status != "inProgress" {
		t.Fatalf("unexpected file change started event: %#v", startedEvent)
	}
	if len(startedEvent.FileChanges) != 2 {
		t.Fatalf("expected structured file changes on start, got %#v", startedEvent.FileChanges)
	}
	if startedEvent.FileChanges[0].Kind != agentproto.FileChangeUpdate || startedEvent.FileChanges[0].MovePath != "new.txt" {
		t.Fatalf("expected rename update payload to be preserved, got %#v", startedEvent.FileChanges[0])
	}
	if startedEvent.FileChanges[1].Kind != agentproto.FileChangeAdd {
		t.Fatalf("expected add payload to be preserved, got %#v", startedEvent.FileChanges[1])
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"file-1","type":"fileChange","status":"completed","changes":[{"path":"old.txt","kind":{"type":"update","move_path":"new.txt"},"diff":"@@ -1 +1 @@\n-old\n+new"},{"path":"removed.txt","kind":{"type":"delete"},"diff":"line 1\nline 2"}]}}}`))
	if err != nil {
		t.Fatalf("observe file change completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one file change completed event, got %#v", completed.Events)
	}
	completedEvent := completed.Events[0]
	if completedEvent.Kind != agentproto.EventItemCompleted || completedEvent.ItemKind != "file_change" || completedEvent.Status != "completed" {
		t.Fatalf("unexpected file change completed event: %#v", completedEvent)
	}
	if len(completedEvent.FileChanges) != 2 {
		t.Fatalf("expected structured file changes on completion, got %#v", completedEvent.FileChanges)
	}
	if completedEvent.FileChanges[0].Kind != agentproto.FileChangeUpdate || completedEvent.FileChanges[0].MovePath != "new.txt" {
		t.Fatalf("expected rename update payload on completion, got %#v", completedEvent.FileChanges[0])
	}
	if completedEvent.FileChanges[1].Kind != agentproto.FileChangeDelete {
		t.Fatalf("expected delete payload on completion, got %#v", completedEvent.FileChanges[1])
	}
}

func TestTranslateThreadsRefreshUsesThreadListAndBuildsSnapshot(t *testing.T) {
	tr := NewTranslator("inst-1")

	commands, err := tr.TranslateCommand(agentproto.Command{Kind: agentproto.CommandThreadsRefresh})
	if err != nil {
		t.Fatalf("translate command: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected one native command, got %d", len(commands))
	}

	var list map[string]any
	if err := json.Unmarshal(commands[0], &list); err != nil {
		t.Fatalf("unmarshal thread/list: %v", err)
	}
	if list["method"] != "thread/list" {
		t.Fatalf("expected thread/list refresh, got %#v", list)
	}
	params, _ := list["params"].(map[string]any)
	if params["sortKey"] != "created_at" {
		t.Fatalf("expected created_at sort key, got %#v", params)
	}

	refreshed, err := tr.ObserveServer([]byte(`{"id":"relay-threads-refresh-0","result":{"data":[{"id":"thread-2","preview":"整理日志"},{"id":"thread-1","name":"修复登录流程","preview":"修登录","state":"idle"}]}}`))
	if err != nil {
		t.Fatalf("observe thread/list response: %v", err)
	}
	if !refreshed.Suppress || len(refreshed.OutboundToCodex) != 2 {
		t.Fatalf("expected suppressed thread/read followups, got %#v", refreshed)
	}

	firstRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-1","result":{"thread":{"id":"thread-2","cwd":"/data/dl/droid","state":"running"}}}`))
	if err != nil {
		t.Fatalf("observe first thread/read: %v", err)
	}
	if !firstRead.Suppress || len(firstRead.Events) != 0 {
		t.Fatalf("expected intermediate thread/read to stay suppressed, got %#v", firstRead)
	}

	secondRead, err := tr.ObserveServer([]byte(`{"id":"relay-thread-read-2","result":{"thread":{"id":"thread-1","cwd":"/data/dl/droid","name":"修复登录流程","preview":"修登录"}}}`))
	if err != nil {
		t.Fatalf("observe second thread/read: %v", err)
	}
	if !secondRead.Suppress || len(secondRead.Events) != 1 {
		t.Fatalf("expected final snapshot event, got %#v", secondRead)
	}
	if secondRead.Events[0].Kind != agentproto.EventThreadsSnapshot || len(secondRead.Events[0].Threads) != 2 {
		t.Fatalf("unexpected snapshot payload: %#v", secondRead.Events[0])
	}
	if secondRead.Events[0].Threads[0].ThreadID != "thread-2" || secondRead.Events[0].Threads[0].CWD != "/data/dl/droid" {
		t.Fatalf("expected snapshot to preserve thread/list order, got %#v", secondRead.Events[0].Threads)
	}
	if secondRead.Events[0].Threads[1].ThreadID != "thread-1" || secondRead.Events[0].Threads[1].Name != "修复登录流程" {
		t.Fatalf("expected thread/read patch to populate title and preserve ordering, got %#v", secondRead.Events[0].Threads)
	}
	if secondRead.Events[0].Threads[0].ListOrder != 1 || secondRead.Events[0].Threads[1].ListOrder != 2 {
		t.Fatalf("expected snapshot records to retain list order metadata, got %#v", secondRead.Events[0].Threads)
	}
}

func TestObserveCommandExecutionItemsCarryCommandMetadata(t *testing.T) {
	tr := NewTranslator("inst-1")

	started, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"cmd-1","type":"commandExecution","status":"inProgress","command":"npm test","cwd":"/tmp/project"}}}`))
	if err != nil {
		t.Fatalf("observe command execution started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one command execution started event, got %#v", started.Events)
	}
	startedEvent := started.Events[0]
	if startedEvent.Kind != agentproto.EventItemStarted || startedEvent.ItemKind != "command_execution" {
		t.Fatalf("unexpected started event: %#v", startedEvent)
	}
	if startedEvent.Metadata["command"] != "npm test" || startedEvent.Metadata["cwd"] != "/tmp/project" {
		t.Fatalf("expected command metadata on start, got %#v", startedEvent.Metadata)
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"cmd-1","type":"command_execution","status":"failed","command":"npm test","cwd":"/tmp/project","exitCode":1}}}`))
	if err != nil {
		t.Fatalf("observe command execution completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one command execution completed event, got %#v", completed.Events)
	}
	completedEvent := completed.Events[0]
	if completedEvent.Kind != agentproto.EventItemCompleted || completedEvent.ItemKind != "command_execution" || completedEvent.Status != "failed" {
		t.Fatalf("unexpected completed event: %#v", completedEvent)
	}
	if completedEvent.Metadata["command"] != "npm test" || completedEvent.Metadata["cwd"] != "/tmp/project" {
		t.Fatalf("expected command metadata on completion, got %#v", completedEvent.Metadata)
	}
	if completedEvent.Metadata["exitCode"] != 1 {
		t.Fatalf("expected exitCode metadata on completion, got %#v", completedEvent.Metadata)
	}
}

func TestObserveClientThreadNameSetResponseEmitsThreadDiscovered(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveClient([]byte(`{"id":"ThreadTitleBackfill:1","method":"thread/name/set","params":{"threadId":"thread-1","name":"修复登录流程"}}`)); err != nil {
		t.Fatalf("observe client thread/name/set: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"id":"ThreadTitleBackfill:1","result":{"ok":true}}`))
	if err != nil {
		t.Fatalf("observe thread/name/set response: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventThreadDiscovered {
		t.Fatalf("expected thread discovered update from successful name set, got %#v", result)
	}
	if result.Events[0].ThreadID != "thread-1" || result.Events[0].Name != "修复登录流程" {
		t.Fatalf("unexpected thread name update event: %#v", result.Events[0])
	}
}

func TestObserveServerImageGenerationLifecycleExtractsStructuredMetadata(t *testing.T) {
	tr := NewTranslator("inst-1")

	started, err := tr.ObserveServer([]byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"img-1","type":"image_generation_call","status":"in_progress","revised_prompt":"a cat in watercolor"}}}`))
	if err != nil {
		t.Fatalf("observe image generation started: %v", err)
	}
	if len(started.Events) != 1 {
		t.Fatalf("expected one image generation started event, got %#v", started.Events)
	}
	startedEvent := started.Events[0]
	if startedEvent.Kind != agentproto.EventItemStarted || startedEvent.ItemKind != "image_generation" {
		t.Fatalf("unexpected image generation started event: %#v", startedEvent)
	}
	if startedEvent.Metadata["revisedPrompt"] != "a cat in watercolor" {
		t.Fatalf("unexpected image generation start metadata: %#v", startedEvent.Metadata)
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"img-1","type":"imageGenerationCall","status":"completed","revisedPrompt":"a cat in watercolor","savedPath":"/tmp/generated.png","result":"aGVsbG8="}}}`))
	if err != nil {
		t.Fatalf("observe image generation completed: %v", err)
	}
	if len(completed.Events) != 1 {
		t.Fatalf("expected one image generation completed event, got %#v", completed.Events)
	}
	completedEvent := completed.Events[0]
	if completedEvent.Kind != agentproto.EventItemCompleted || completedEvent.ItemKind != "image_generation" {
		t.Fatalf("unexpected image generation completed event: %#v", completedEvent)
	}
	if completedEvent.Metadata["revisedPrompt"] != "a cat in watercolor" {
		t.Fatalf("unexpected completed image prompt metadata: %#v", completedEvent.Metadata)
	}
	if completedEvent.Metadata["savedPath"] != "/tmp/generated.png" {
		t.Fatalf("unexpected completed image saved path metadata: %#v", completedEvent.Metadata)
	}
	if completedEvent.Metadata["imageBase64"] != "aGVsbG8=" {
		t.Fatalf("unexpected completed image base64 metadata: %#v", completedEvent.Metadata)
	}
}
