package acp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const (
	acpProtocolVersion = 1
	clientName         = "codex-remote-feishu"
)

type Result struct {
	Events                   []agentproto.Event
	OutboundToChild          [][]byte
	OutboundToParent         [][]byte
	ResolvedCommandResponses []ResolvedCommandResponse
	Suppress                 bool
}

type ResolvedCommandResponse struct {
	RequestID     string
	RejectMessage string
}

type Translator struct {
	instanceID    string
	workspaceRoot string
	nextID        int
	debugLog      func(string, ...any)

	currentSessionID string
	sessions         map[string]sessionState
	activeTurns      map[string]*turnState
	messageItems     map[string]*itemState
	threadUsage      map[string]*agentproto.ThreadTokenUsage

	pendingRPC         map[string]pendingRPC
	pendingPermissions map[string]pendingPermission
	writeApprovals     map[string]writeApproval
	historyHydrations  map[string]*historyHydrationState
}

type sessionState struct {
	ID            string
	CWD           string
	Title         string
	ModelOptions  []modelOption
	CurrentModel  string
	CurrentMode   string
	ConfigOptions []map[string]any
}

type modelOption struct {
	Value string
	Name  string
}

type pendingRPC struct {
	Kind    string
	Command agentproto.Command
	Turn    *turnState
}

type pendingPermission struct {
	NativeID any
	ThreadID string
	TurnID   string
	ItemID   string
	Options  []permissionOption
}

type permissionOption struct {
	ID    string
	Kind  string
	Label string
}

type writeApproval struct {
	Always    bool
	Remaining int
}

type turnState struct {
	CommandID  string
	Initiator  agentproto.Initiator
	ThreadID   string
	TurnID     string
	StartedAt  time.Time
	Completed  bool
	Traffic    agentproto.TrafficClass
	MessageIDs map[string]bool
}

type itemState struct {
	ItemID   string
	Kind     string
	ThreadID string
	TurnID   string
	Started  bool
	Text     strings.Builder
}

type historyHydrationState struct {
	CommandID string
	ThreadID  string
	CWD       string
	Turns     []agentproto.ThreadHistoryTurnRecord
	current   int
	itemByKey map[string]historyItemRef
}

type historyItemRef struct {
	Turn int
	Item int
}

func NewTranslator(instanceID, workspaceRoot string) *Translator {
	return &Translator{
		instanceID:         strings.TrimSpace(instanceID),
		workspaceRoot:      strings.TrimSpace(workspaceRoot),
		nextID:             1,
		sessions:           map[string]sessionState{},
		activeTurns:        map[string]*turnState{},
		messageItems:       map[string]*itemState{},
		threadUsage:        map[string]*agentproto.ThreadTokenUsage{},
		pendingRPC:         map[string]pendingRPC{},
		pendingPermissions: map[string]pendingPermission{},
		writeApprovals:     map[string]writeApproval{},
		historyHydrations:  map[string]*historyHydrationState{},
	}
}

func (t *Translator) SetDebugLogger(debugLog func(string, ...any)) {
	t.debugLog = debugLog
}

func (t *Translator) debugf(format string, args ...any) {
	if t.debugLog != nil {
		t.debugLog(format, args...)
	}
}

func (t *Translator) BuildInitializeFrame() ([]byte, error) {
	requestID := t.nextRequest("initialize")
	t.pendingRPC[requestID] = pendingRPC{Kind: "initialize"}
	return marshalLine(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": acpProtocolVersion,
			"clientCapabilities": map[string]any{
				"_meta": map[string]any{
					"terminal-auth": true,
				},
			},
			"clientInfo": map[string]any{
				"name":    clientName,
				"version": "0.1.0",
			},
		},
	})
}

func (t *Translator) ObserveClient(_ []byte) (Result, error) {
	return Result{}, nil
}

func (t *Translator) ObserveServer(line []byte) (Result, error) {
	var frame map[string]any
	if err := json.Unmarshal(line, &frame); err != nil {
		return Result{}, err
	}
	result := Result{Suppress: true}
	if method := strings.TrimSpace(xutil.LookupStringFromAny(frame["method"])); method != "" {
		return t.observeMethodFrame(frame, method, result)
	}
	if _, ok := frame["id"]; ok {
		return t.observeResponseFrame(frame, result)
	}
	return result, nil
}

func (t *Translator) TranslateCommand(command agentproto.Command) (Result, error) {
	switch command.Kind {
	case agentproto.CommandPromptSend:
		return t.translatePromptSend(command)
	case agentproto.CommandTurnInterrupt:
		return t.translateTurnInterrupt(command)
	case agentproto.CommandRequestRespond:
		return t.translateRequestRespond(command)
	case agentproto.CommandThreadsRefresh:
		return t.translateThreadsRefresh(command)
	case agentproto.CommandThreadHistoryRead:
		return t.translateThreadHistoryRead(command)
	case agentproto.CommandModelList:
		return t.translateModelList(command)
	case agentproto.CommandTurnSteer:
		return Result{}, agentproto.ErrorInfo{
			Code:             "opencode_steer_not_supported",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "OpenCode runtime 当前不支持把补充输入并入 active turn。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	default:
		return Result{}, agentproto.ErrorInfo{
			Code:             "opencode_command_not_supported_yet",
			Layer:            "wrapper",
			Stage:            "translate_command",
			Operation:        string(command.Kind),
			Message:          "当前 OpenCode runtime 尚未支持该命令。",
			SurfaceSessionID: command.Origin.Surface,
			CommandID:        command.CommandID,
			ThreadID:         command.Target.ThreadID,
			TurnID:           command.Target.TurnID,
		}
	}
}

func (t *Translator) upsertSession(sessionID, cwd string, payload map[string]any) sessionState {
	session := t.sessions[sessionID]
	session.ID = sessionID
	session.CWD = xutil.FirstNonEmpty(cwd, session.CWD, t.workspaceRoot)
	session.Title = xutil.FirstNonEmpty(xutil.LookupStringFromAny(payload["title"]), session.Title)
	if options := xutil.MapsFromAny(payload["configOptions"]); len(options) != 0 {
		session.ConfigOptions = options
		session.ModelOptions, session.CurrentModel, session.CurrentMode = parseConfigOptions(options)
	}
	t.sessions[sessionID] = session
	return session
}

func (t *Translator) newTurn(sessionID string, command agentproto.Command) *turnState {
	turnID := "opencode-turn-" + strconv.Itoa(t.nextID)
	t.nextID++
	initiator := commandInitiator(command)
	traffic := agentproto.TrafficClass("")
	if command.Target.InternalHelper {
		traffic = agentproto.TrafficClassInternalHelper
	}
	return &turnState{
		CommandID:  command.CommandID,
		Initiator:  initiator,
		ThreadID:   sessionID,
		TurnID:     turnID,
		StartedAt:  time.Now().UTC(),
		Traffic:    traffic,
		MessageIDs: map[string]bool{},
	}
}

func (t *Translator) ensureTurnForSession(sessionID string) *turnState {
	if sessionID == "" {
		return nil
	}
	if turn := t.activeTurns[sessionID]; turn != nil {
		return turn
	}
	turn := &turnState{
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
		ThreadID:  sessionID,
		TurnID:    "opencode-observed-turn-" + strconv.Itoa(t.nextID),
		StartedAt: time.Now().UTC(),
	}
	t.nextID++
	t.activeTurns[sessionID] = turn
	return turn
}

func (t *Translator) annotateTurnEvent(turn *turnState, event agentproto.Event) agentproto.Event {
	if turn == nil {
		return event
	}
	event.CommandID = xutil.FirstNonEmpty(event.CommandID, turn.CommandID)
	event.Initiator = turn.Initiator
	if turn.Traffic != "" {
		event.TrafficClass = turn.Traffic
	}
	return event
}

func (t *Translator) buildPromptContent(inputs []agentproto.Input) ([]map[string]any, error) {
	if len(inputs) == 0 {
		return []map[string]any{{"type": "text", "text": ""}}, nil
	}
	output := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		switch input.Type {
		case agentproto.InputText:
			output = append(output, map[string]any{"type": "text", "text": input.Text})
		case agentproto.InputLocalImage:
			image, err := buildImageContent(input)
			if err != nil {
				return nil, err
			}
			output = append(output, image)
		case agentproto.InputRemoteImage:
			return nil, agentproto.ErrorInfo{
				Code:      "opencode_remote_image_not_supported",
				Layer:     "wrapper",
				Stage:     "translate_command",
				Operation: string(agentproto.CommandPromptSend),
				Message:   "OpenCode runtime 当前只支持文本与本地图片输入。",
			}
		default:
			return nil, agentproto.ErrorInfo{
				Code:      "opencode_prompt_input_not_supported",
				Layer:     "wrapper",
				Stage:     "translate_command",
				Operation: string(agentproto.CommandPromptSend),
				Message:   "OpenCode runtime 当前只支持文本与本地图片输入。",
			}
		}
	}
	return output, nil
}

func buildImageContent(input agentproto.Input) (map[string]any, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return nil, fmt.Errorf("local image input requires path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	mimeType := strings.TrimSpace(input.MIMEType)
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(path))
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	return map[string]any{
		"type":     "image",
		"mimeType": mimeType,
		"data":     base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (t *Translator) commandCWD(command agentproto.Command) string {
	return xutil.FirstNonEmpty(command.Target.CWD, t.workspaceRoot)
}

func (t *Translator) sessionWorkspace(sessionID string) string {
	session := t.sessions[sessionID]
	return xutil.FirstNonEmpty(session.CWD, t.workspaceRoot)
}

func resolveWorkspaceWritePath(workspaceRoot, target string) (string, string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	target = strings.TrimSpace(target)
	if workspaceRoot == "" {
		return "", "", fmt.Errorf("workspace root is required")
	}
	if target == "" {
		return "", "", fmt.Errorf("write path is required")
	}
	baseAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("fs/write_text_file path is outside workspace")
	}
	return targetAbs, filepath.ToSlash(rel), nil
}

func simpleTextDiff(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("--- ")
	builder.WriteString(path)
	builder.WriteByte('\n')
	builder.WriteString("+++ ")
	builder.WriteString(path)
	builder.WriteByte('\n')
	builder.WriteString("@@\n")
	for _, line := range strings.Split(strings.TrimSuffix(oldText, "\n"), "\n") {
		if line == "" && oldText == "" {
			continue
		}
		builder.WriteByte('-')
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	for _, line := range strings.Split(strings.TrimSuffix(newText, "\n"), "\n") {
		if line == "" && newText == "" {
			continue
		}
		builder.WriteByte('+')
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String()
}

func (t *Translator) nextRequest(prefix string) string {
	value := fmt.Sprintf("relay-%s-%d", prefix, t.nextID)
	t.nextID++
	return value
}

func commandInitiator(command agentproto.Command) agentproto.Initiator {
	if command.Target.InternalHelper {
		return agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper}
	}
	surfaceID := xutil.FirstNonEmpty(command.Origin.Surface, command.Origin.ChatID)
	if surfaceID == "" {
		return agentproto.Initiator{Kind: agentproto.InitiatorUnknown}
	}
	return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID}
}

func marshalLine(payload map[string]any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func idKey(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return xutil.Stringify(typed)
	}
}

func statusFromStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "cancelled":
		return "cancelled"
	case "max_tokens", "refusal":
		return "failed"
	default:
		return "completed"
	}
}

func promptUsage(value any) (agentproto.TokenUsageBreakdown, bool) {
	raw, _ := value.(map[string]any)
	if raw == nil {
		return agentproto.TokenUsageBreakdown{}, false
	}
	usage := agentproto.TokenUsageBreakdown{
		InputTokens:  xutil.LookupIntFromAny(raw["inputTokens"]),
		OutputTokens: xutil.LookupIntFromAny(raw["outputTokens"]),
		TotalTokens:  xutil.LookupIntFromAny(raw["totalTokens"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage, usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.TotalTokens != 0
}

func parseConfigOptions(options []map[string]any) ([]modelOption, string, string) {
	var models []modelOption
	currentModel := ""
	currentMode := ""
	for _, option := range options {
		switch xutil.LookupStringFromAny(option["id"]) {
		case "model":
			currentModel = xutil.LookupStringFromAny(option["currentValue"])
			models = parseModelOptions(option["options"])
		case "mode":
			currentMode = xutil.LookupStringFromAny(option["currentValue"])
		}
	}
	return models, currentModel, currentMode
}

func parseModelOptions(value any) []modelOption {
	var out []modelOption
	for _, item := range flattenOptionItems(value) {
		option := modelOption{
			Value: xutil.LookupStringFromAny(item["value"]),
			Name:  xutil.LookupStringFromAny(item["name"]),
		}
		if option.Value != "" {
			out = append(out, option)
		}
	}
	return out
}

func flattenOptionItems(value any) []map[string]any {
	items := xutil.MapsFromAny(value)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if _, ok := item["value"]; ok {
			out = append(out, item)
			continue
		}
		out = append(out, flattenOptionItems(item["options"])...)
	}
	return out
}

func parsePermissionOptions(value any) []permissionOption {
	rawOptions, _ := value.([]any)
	out := make([]permissionOption, 0, len(rawOptions))
	for _, raw := range rawOptions {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		option := permissionOption{
			ID:    xutil.LookupStringFromAny(item["optionId"]),
			Kind:  xutil.LookupStringFromAny(item["kind"]),
			Label: xutil.LookupStringFromAny(item["name"]),
		}
		if option.ID != "" {
			out = append(out, option)
		}
	}
	return out
}

func permissionOptionStyle(option permissionOption) string {
	if strings.Contains(option.Kind, "reject") || strings.EqualFold(option.ID, "reject") {
		return "danger"
	}
	return "primary"
}

func resolvePermissionOptionID(response map[string]any) string {
	if id := strings.TrimSpace(xutil.LookupStringFromAny(response["optionId"])); id != "" {
		return id
	}
	if decision := strings.TrimSpace(xutil.LookupStringFromAny(response["decision"])); decision != "" {
		switch decision {
		case "accept", "approved", "allow", "once":
			return "once"
		case "decline", "reject", "denied":
			return "reject"
		default:
			return decision
		}
	}
	if approved, ok := response["approved"].(bool); ok {
		if approved {
			return "once"
		}
		return "reject"
	}
	return ""
}

func toolItemKind(update map[string]any) string {
	switch strings.TrimSpace(xutil.LookupStringFromAny(update["kind"])) {
	case "execute":
		return "command_execution"
	case "edit":
		return "file_change"
	case "fetch":
		return "web_fetch"
	default:
		return "tool_call"
	}
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
