package execprogress

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const ExplorationBlockID = "exploration"

type ExplorationDisposition string

const (
	ExplorationDispositionPending    ExplorationDisposition = "pending"
	ExplorationDispositionStructured ExplorationDisposition = "structured"
	ExplorationDispositionGeneric    ExplorationDisposition = "generic"
)

type ExplorationProgressResult struct {
	Disposition         ExplorationDisposition
	Changed             bool
	ForceGenericOnFinal bool
}

type explorationCandidate string

const (
	explorationCandidateUnavailable explorationCandidate = "unavailable"
	explorationCandidatePending     explorationCandidate = "pending"
	explorationCandidateStructured  explorationCandidate = "structured"
	explorationCandidateRejected    explorationCandidate = "rejected"
)

type explorationAction struct {
	Kind      string
	Items     []string
	Summary   string
	Secondary string
	MergeKey  string
}

type ExplorationAction = explorationAction

var explorationShellLCCommandPattern = regexp.MustCompile(`^(?:/usr/bin/|/bin/)?(?:bash|sh|zsh)\s+-lc\s+(.+)$`)

func ResolveExplorationProgressForCommandExecution(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) ExplorationProgressResult {
	actions, disposition := explorationActionsForCommandExecution(event, final)
	return resolveExplorationProgress(progress, event.ItemID, actions, disposition, NormalizeStatus(event.Status, final), final)
}

func ResolveExplorationProgressForDynamicTool(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) ExplorationProgressResult {
	actions, disposition := explorationActionsForDynamicTool(event, final)
	status := NormalizeDynamicToolProgressStatus(event)
	if final && status == "" {
		status = "completed"
	}
	return resolveExplorationProgress(progress, event.ItemID, actions, disposition, status, final)
}

func ParseCommandExecutionExplorationAction(command string) (ExplorationAction, bool) {
	return parseCommandExecutionExplorationAction(command)
}

func explorationActionsForCommandExecution(event agentproto.Event, final bool) ([]explorationAction, explorationCandidate) {
	if event.Exploration != nil {
		return normalizeTypedExplorationActions(event.Exploration, "command_execution", final)
	}
	command, _ := CommandMetadata(event)
	action, ok := parseCommandExecutionExplorationAction(command)
	if !ok {
		return nil, explorationCandidateUnavailable
	}
	return []explorationAction{action}, explorationCandidateStructured
}

func explorationActionsForDynamicTool(event agentproto.Event, final bool) ([]explorationAction, explorationCandidate) {
	if event.Exploration != nil {
		return normalizeTypedExplorationActions(event.Exploration, "dynamic_tool_call", final)
	}
	action, ok := parseDynamicToolExplorationAction(event.Metadata)
	if ok {
		return []explorationAction{action}, explorationCandidateStructured
	}
	if strings.EqualFold(strings.TrimSpace(xutil.MetadataString(event.Metadata, "tool")), "read") {
		if final {
			return nil, explorationCandidateRejected
		}
		return nil, explorationCandidatePending
	}
	return nil, explorationCandidateUnavailable
}

func normalizeTypedExplorationActions(contract *agentproto.ExplorationActions, source string, final bool) ([]explorationAction, explorationCandidate) {
	if contract == nil || len(contract.Actions) == 0 {
		return nil, explorationCandidateRejected
	}
	actions := make([]explorationAction, 0, len(contract.Actions))
	incomplete := false
	for _, raw := range contract.Actions {
		action := explorationAction{
			Kind:      strings.ToLower(strings.TrimSpace(string(raw.Kind))),
			Items:     appendUniquePreserveOrder(nil, trimmedNonEmptyStrings(raw.Items)...),
			Summary:   strings.TrimSpace(raw.Summary),
			Secondary: strings.TrimSpace(raw.Secondary),
		}
		switch action.Kind {
		case "read":
			incomplete = incomplete || len(action.Items) == 0
		case "list", "search":
			incomplete = incomplete || action.Summary == ""
		default:
			return nil, explorationCandidateRejected
		}
		action.MergeKey = strings.TrimSpace(source) + ":" + action.Kind
		actions = append(actions, action)
	}
	if incomplete {
		if final {
			return nil, explorationCandidateRejected
		}
		return nil, explorationCandidatePending
	}
	return actions, explorationCandidateStructured
}

func trimmedNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveExplorationProgress(progress *state.ExecCommandProgressRecord, itemID string, actions []explorationAction, candidate explorationCandidate, status string, final bool) ExplorationProgressResult {
	if progress == nil {
		return ExplorationProgressResult{Disposition: ExplorationDispositionGeneric}
	}
	itemID = strings.TrimSpace(itemID)
	existing := explorationItemDisposition(progress, itemID)
	switch existing {
	case ExplorationDispositionStructured:
		changed := updateExplorationItemLifecycle(progress, itemID, status, final)
		return ExplorationProgressResult{Disposition: existing, Changed: changed}
	case ExplorationDispositionGeneric:
		return ExplorationProgressResult{Disposition: existing}
	}
	if itemID == "" && candidate != explorationCandidateUnavailable && candidate != explorationCandidateRejected {
		candidate = explorationCandidateRejected
	}
	forceGenericOnFinal := final && (candidate == explorationCandidateRejected || existing == ExplorationDispositionPending)
	switch candidate {
	case explorationCandidatePending:
		setExplorationItemDisposition(progress, itemID, ExplorationDispositionPending)
		markPendingExplorationItemActive(progress, itemID)
		return ExplorationProgressResult{Disposition: ExplorationDispositionPending}
	case explorationCandidateStructured:
		exploration := ensureExplorationProgress(progress)
		before := cloneExplorationBlock(exploration.Block)
		for _, action := range actions {
			appendExplorationRow(progress, &exploration.Block, action)
		}
		setExplorationItemDisposition(progress, itemID, ExplorationDispositionStructured)
		updateExplorationItemLifecycle(progress, itemID, status, final)
		return ExplorationProgressResult{
			Disposition: ExplorationDispositionStructured,
			Changed:     !sameExecCommandProgressBlock(before, exploration.Block),
		}
	case explorationCandidateRejected, explorationCandidateUnavailable:
		changed := clearPendingExplorationItem(progress, itemID, status)
		setExplorationItemDisposition(progress, itemID, ExplorationDispositionGeneric)
		return ExplorationProgressResult{
			Disposition:         ExplorationDispositionGeneric,
			Changed:             changed,
			ForceGenericOnFinal: forceGenericOnFinal,
		}
	}
	return ExplorationProgressResult{Disposition: ExplorationDispositionGeneric}
}

func explorationItemDisposition(progress *state.ExecCommandProgressRecord, itemID string) ExplorationDisposition {
	if progress == nil || itemID == "" || progress.ExplorationItems == nil {
		return ""
	}
	return ExplorationDisposition(progress.ExplorationItems[itemID])
}

func setExplorationItemDisposition(progress *state.ExecCommandProgressRecord, itemID string, disposition ExplorationDisposition) {
	if progress == nil || itemID == "" || disposition == "" {
		return
	}
	if progress.ExplorationItems == nil {
		progress.ExplorationItems = map[string]string{}
	}
	progress.ExplorationItems[itemID] = string(disposition)
}

func updateExplorationItemLifecycle(progress *state.ExecCommandProgressRecord, itemID, status string, final bool) bool {
	if progress == nil {
		return false
	}
	exploration := ensureExplorationProgress(progress)
	before := cloneExplorationBlock(exploration.Block)
	if exploration.ActiveItemIDs == nil {
		exploration.ActiveItemIDs = map[string]bool{}
	}
	if itemID != "" {
		if final {
			delete(exploration.ActiveItemIDs, itemID)
		} else {
			exploration.ActiveItemIDs[itemID] = true
		}
	}
	if status == "failed" {
		exploration.Failed = true
	}
	exploration.Block.Status = explorationBlockStatus(exploration)
	return !sameExecCommandProgressBlock(before, exploration.Block)
}

func markPendingExplorationItemActive(progress *state.ExecCommandProgressRecord, itemID string) {
	if progress == nil || progress.Exploration == nil || strings.TrimSpace(itemID) == "" {
		return
	}
	progress.Exploration.ActiveItemIDs[strings.TrimSpace(itemID)] = true
	progress.Exploration.Block.Status = explorationBlockStatus(progress.Exploration)
}

func clearPendingExplorationItem(progress *state.ExecCommandProgressRecord, itemID, status string) bool {
	if progress == nil || progress.Exploration == nil {
		return false
	}
	before := cloneExplorationBlock(progress.Exploration.Block)
	delete(progress.Exploration.ActiveItemIDs, strings.TrimSpace(itemID))
	if status == "failed" && len(progress.Exploration.Block.Rows) != 0 {
		progress.Exploration.Failed = true
	}
	progress.Exploration.Block.Status = explorationBlockStatus(progress.Exploration)
	return !sameExecCommandProgressBlock(before, progress.Exploration.Block)
}

func ensureExplorationProgress(progress *state.ExecCommandProgressRecord) *state.ExecCommandProgressExplorationRecord {
	if progress.Exploration == nil {
		progress.Exploration = &state.ExecCommandProgressExplorationRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: ExplorationBlockID,
				Kind:    "exploration",
				Status:  "running",
			},
			ActiveItemIDs: map[string]bool{},
		}
	}
	if progress.Exploration.Block.BlockID == "" {
		progress.Exploration.Block.BlockID = ExplorationBlockID
	}
	if progress.Exploration.Block.Kind == "" {
		progress.Exploration.Block.Kind = "exploration"
	}
	if progress.Exploration.ActiveItemIDs == nil {
		progress.Exploration.ActiveItemIDs = map[string]bool{}
	}
	for itemID, disposition := range progress.ExplorationItems {
		if ExplorationDisposition(disposition) == ExplorationDispositionPending {
			progress.Exploration.ActiveItemIDs[itemID] = true
		}
	}
	return progress.Exploration
}

func appendExplorationRow(progress *state.ExecCommandProgressRecord, block *state.ExecCommandProgressBlockRecord, action explorationAction) {
	if progress == nil || block == nil {
		return
	}
	action.Kind = strings.TrimSpace(action.Kind)
	action.Summary = strings.TrimSpace(action.Summary)
	action.Secondary = strings.TrimSpace(action.Secondary)
	action.MergeKey = strings.TrimSpace(action.MergeKey)
	items := make([]string, 0, len(action.Items))
	for _, item := range action.Items {
		if text := strings.TrimSpace(item); text != "" {
			items = append(items, text)
		}
	}
	if action.Kind == "" {
		return
	}
	if action.Kind == "read" {
		if len(items) == 0 {
			return
		}
		if len(block.Rows) > 0 && canMergeReadExplorationRow(progress, block.Rows[len(block.Rows)-1], action.MergeKey) {
			last := &block.Rows[len(block.Rows)-1]
			last.Items = appendUniquePreserveOrder(last.Items, items...)
			last.MergeKey = normalizeExplorationMergeKey(action.Kind, action.MergeKey)
			progress.LastVisibleSeq++
			last.LastSeq = progress.LastVisibleSeq
			return
		}
		progress.LastVisibleSeq++
		block.Rows = append(block.Rows, state.ExecCommandProgressBlockRowRecord{
			RowID:    nextExplorationRowID(block, action.Kind),
			Kind:     action.Kind,
			Items:    append([]string(nil), items...),
			MergeKey: normalizeExplorationMergeKey(action.Kind, action.MergeKey),
			LastSeq:  progress.LastVisibleSeq,
		})
		return
	}
	progress.LastVisibleSeq++
	block.Rows = append(block.Rows, state.ExecCommandProgressBlockRowRecord{
		RowID:     nextExplorationRowID(block, action.Kind),
		Kind:      action.Kind,
		Items:     append([]string(nil), items...),
		Summary:   action.Summary,
		Secondary: action.Secondary,
		MergeKey:  normalizeExplorationMergeKey(action.Kind, action.MergeKey),
		LastSeq:   progress.LastVisibleSeq,
	})
}

func canMergeReadExplorationRow(progress *state.ExecCommandProgressRecord, row state.ExecCommandProgressBlockRowRecord, mergeKey string) bool {
	if progress == nil {
		return false
	}
	if strings.TrimSpace(row.Kind) != "read" {
		return false
	}
	if row.LastSeq != progress.LastVisibleSeq {
		return false
	}
	return normalizeExplorationMergeKey("read", row.MergeKey) == normalizeExplorationMergeKey("read", mergeKey)
}

func normalizeExplorationMergeKey(kind, mergeKey string) string {
	kind = strings.TrimSpace(kind)
	mergeKey = strings.TrimSpace(mergeKey)
	if mergeKey != "" {
		return mergeKey
	}
	return kind
}

func nextExplorationRowID(block *state.ExecCommandProgressBlockRecord, kind string) string {
	return strings.TrimSpace(kind) + "::" + strconv.Itoa(len(block.Rows)+1)
}

func explorationBlockStatus(exploration *state.ExecCommandProgressExplorationRecord) string {
	if exploration == nil {
		return ""
	}
	for _, active := range exploration.ActiveItemIDs {
		if active {
			return "running"
		}
	}
	if exploration.Failed {
		return "failed"
	}
	return "completed"
}

func cloneExplorationBlock(block state.ExecCommandProgressBlockRecord) state.ExecCommandProgressBlockRecord {
	clone := state.ExecCommandProgressBlockRecord{
		BlockID: block.BlockID,
		Kind:    block.Kind,
		Status:  block.Status,
		Rows:    make([]state.ExecCommandProgressBlockRowRecord, 0, len(block.Rows)),
	}
	for _, row := range block.Rows {
		clone.Rows = append(clone.Rows, state.ExecCommandProgressBlockRowRecord{
			RowID:     row.RowID,
			Kind:      row.Kind,
			Items:     append([]string(nil), row.Items...),
			Summary:   row.Summary,
			Secondary: row.Secondary,
		})
	}
	return clone
}

func sameExecCommandProgressBlock(left, right state.ExecCommandProgressBlockRecord) bool {
	if left.BlockID != right.BlockID || left.Kind != right.Kind || left.Status != right.Status || len(left.Rows) != len(right.Rows) {
		return false
	}
	for i := range left.Rows {
		if !sameExecCommandProgressBlockRow(left.Rows[i], right.Rows[i]) {
			return false
		}
	}
	return true
}

func sameExecCommandProgressBlockRow(left, right state.ExecCommandProgressBlockRowRecord) bool {
	if left.RowID != right.RowID || left.Kind != right.Kind || left.Summary != right.Summary || left.Secondary != right.Secondary {
		return false
	}
	return sameStringSlice(left.Items, right.Items)
}

func parseDynamicToolExplorationAction(metadata map[string]any) (explorationAction, bool) {
	tool := strings.ToLower(strings.TrimSpace(xutil.MetadataString(metadata, "tool")))
	switch tool {
	case "read":
		items := dynamicToolProgressArguments(metadata)
		if len(items) == 0 {
			if summary := strings.TrimSpace(dynamicToolProgressSummaryFromMetadata(metadata)); summary != "" {
				items = []string{summary}
			}
		}
		if len(items) == 0 {
			return explorationAction{}, false
		}
		return explorationAction{Kind: "read", Items: items, MergeKey: "dynamic_tool:read"}, true
	default:
		return explorationAction{}, false
	}
}

func parseCommandExecutionExplorationAction(command string) (explorationAction, bool) {
	command = normalizeExplorationCommand(command)
	if command == "" {
		return explorationAction{}, false
	}
	args, hasShellOperators, ok := splitExplorationShellWords(command)
	if !ok || hasShellOperators || len(args) == 0 {
		return explorationAction{}, false
	}
	cmd := strings.ToLower(strings.TrimSpace(filepath.Base(args[0])))
	switch cmd {
	case "cat", "bat":
		items := positionalArgs(args[1:], nil)
		if len(items) == 0 {
			return explorationAction{}, false
		}
		return explorationAction{Kind: "read", Items: items, MergeKey: "command_execution:" + cmd}, true
	case "head", "tail":
		if item := lastPositionalArg(args[1:]); item != "" {
			return explorationAction{Kind: "read", Items: []string{item}, MergeKey: "command_execution:" + cmd}, true
		}
	case "sed":
		if item := lastPositionalArg(args[1:]); item != "" {
			return explorationAction{Kind: "read", Items: []string{item}, MergeKey: "command_execution:" + cmd}, true
		}
	case "ls":
		items := positionalArgs(args[1:], nil)
		if len(items) == 0 {
			return explorationAction{Kind: "list", Summary: command}, true
		}
		return explorationAction{Kind: "list", Summary: strings.Join(items, " ")}, true
	case "fd", "fdfind", "find":
		return explorationAction{Kind: "list", Summary: command}, true
	case "rg", "grep":
		if cmd == "rg" && commandHasStandaloneOption(args[1:], "--files") {
			return explorationAction{Kind: "list", Summary: command}, true
		}
		query, scope := parseSearchArgs(args[1:])
		if query == "" {
			return explorationAction{}, false
		}
		return explorationAction{Kind: "search", Summary: query, Secondary: scope}, true
	}
	return explorationAction{}, false
}

func commandHasStandaloneOption(args []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == target {
			return true
		}
	}
	return false
}

func normalizeExplorationCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	match := explorationShellLCCommandPattern.FindStringSubmatch(command)
	if len(match) == 2 {
		if words, hasShellOperators, ok := splitExplorationShellWords(match[1]); ok && !hasShellOperators && len(words) == 1 {
			return strings.TrimSpace(words[0])
		}
		command = strings.TrimSpace(match[1])
	}
	if len(command) > 0 && (command[0] == '"' || command[0] == '\'') {
		if words, hasShellOperators, ok := splitExplorationShellWords(command); ok && !hasShellOperators && len(words) == 1 {
			return strings.TrimSpace(words[0])
		}
	}
	return command
}

func splitExplorationShellWords(command string) ([]string, bool, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, false, true
	}
	var (
		words            []string
		current          strings.Builder
		inSingleQuote    bool
		inDoubleQuote    bool
		hasShellOperator bool
	)
	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		words = append(words, current.String())
		current.Reset()
	}
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if inSingleQuote {
			if ch == '\'' {
				inSingleQuote = false
				continue
			}
			current.WriteByte(ch)
			continue
		}
		if inDoubleQuote {
			switch ch {
			case '"':
				inDoubleQuote = false
			case '\\':
				if i+1 >= len(command) {
					current.WriteByte(ch)
					continue
				}
				next := command[i+1]
				switch next {
				case '"', '\\', '$', '`':
					current.WriteByte(next)
					i++
				case '\n':
					i++
				default:
					current.WriteByte(ch)
				}
			default:
				current.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case ' ', '\t', '\n', '\r':
			flushCurrent()
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '\\':
			if i+1 >= len(command) {
				current.WriteByte(ch)
				continue
			}
			i++
			current.WriteByte(command[i])
		case '|':
			flushCurrent()
			hasShellOperator = true
			if i+1 < len(command) && command[i+1] == '|' {
				i++
			}
		case ';':
			flushCurrent()
			hasShellOperator = true
		case '>':
			flushCurrent()
			hasShellOperator = true
			if i+1 < len(command) && (command[i+1] == '>' || command[i+1] == '&') {
				i++
			}
		case '<':
			flushCurrent()
			hasShellOperator = true
			if i+1 < len(command) && (command[i+1] == '<' || command[i+1] == '&') {
				i++
			}
		case '&':
			if i+1 < len(command) && command[i+1] == '&' {
				flushCurrent()
				hasShellOperator = true
				i++
				continue
			}
			current.WriteByte(ch)
		default:
			current.WriteByte(ch)
		}
	}
	if inSingleQuote || inDoubleQuote {
		return nil, false, false
	}
	flushCurrent()
	return words, hasShellOperator, true
}

func positionalArgs(args []string, optionsWithValue map[string]bool) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if optionsWithValue != nil && optionsWithValue[arg] {
				skipNext = true
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

func lastPositionalArg(args []string) string {
	positional := positionalArgs(args, map[string]bool{
		"-n":          true,
		"-c":          true,
		"-m":          true,
		"--lines":     true,
		"--bytes":     true,
		"--max-count": true,
	})
	if len(positional) == 0 {
		return ""
	}
	return strings.TrimSpace(positional[len(positional)-1])
}

func parseSearchArgs(args []string) (string, string) {
	positional := positionalArgs(args, map[string]bool{
		"-e":               true,
		"-f":               true,
		"-g":               true,
		"-m":               true,
		"-C":               true,
		"-A":               true,
		"-B":               true,
		"-t":               true,
		"--regexp":         true,
		"--file":           true,
		"--glob":           true,
		"--max-count":      true,
		"--context":        true,
		"--after-context":  true,
		"--before-context": true,
		"--type":           true,
	})
	if len(positional) == 0 {
		return "", ""
	}
	query := strings.TrimSpace(positional[0])
	scope := ""
	if len(positional) > 1 {
		scope = strings.TrimSpace(positional[len(positional)-1])
		if scope == query {
			scope = ""
		}
	}
	return query, scope
}
