package claudeutil

import "testing"

func TestClaudeExplorationActions(t *testing.T) {
	cases := []struct {
		name          string
		tool          string
		input         map[string]any
		wantKind      string
		wantItems     []string
		wantSummary   string
		wantSecondary string
	}{
		{
			name:      "read uses file_path and ignores paging noise",
			tool:      "Read",
			input:     map[string]any{"file_path": "internal/claudeutil/claude.go", "offset": 20, "limit": 40},
			wantKind:  "read",
			wantItems: []string{"internal/claudeutil/claude.go"},
		},
		{
			name:        "glob prefers path scope",
			tool:        "Glob",
			input:       map[string]any{"path": "internal/adapter/claude", "pattern": "**/*_test.go"},
			wantKind:    "list",
			wantSummary: "internal/adapter/claude",
		},
		{
			name:        "glob pattern only keeps pattern as pattern",
			tool:        "Glob",
			input:       map[string]any{"pattern": "**/*.go"},
			wantKind:    "list",
			wantSummary: "**/*.go",
		},
		{
			name:          "grep uses pattern and optional path",
			tool:          "Grep",
			input:         map[string]any{"pattern": "ClaudeToolMetadata", "path": "internal/claudeutil", "output_mode": "content"},
			wantKind:      "search",
			wantSummary:   "ClaudeToolMetadata",
			wantSecondary: "internal/claudeutil",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ClaudeExplorationActions(tt.tool, tt.input)
			if got == nil || len(got.Actions) != 1 {
				t.Fatalf("ClaudeExplorationActions(%q) = %#v, want one action", tt.tool, got)
			}
			action := got.Actions[0]
			if string(action.Kind) != tt.wantKind {
				t.Fatalf("kind = %q, want %q", action.Kind, tt.wantKind)
			}
			if len(action.Items) != len(tt.wantItems) {
				t.Fatalf("items = %#v, want %#v", action.Items, tt.wantItems)
			}
			for i := range tt.wantItems {
				if action.Items[i] != tt.wantItems[i] {
					t.Fatalf("items = %#v, want %#v", action.Items, tt.wantItems)
				}
			}
			if action.Summary != tt.wantSummary {
				t.Fatalf("summary = %q, want %q", action.Summary, tt.wantSummary)
			}
			if action.Secondary != tt.wantSecondary {
				t.Fatalf("secondary = %q, want %q", action.Secondary, tt.wantSecondary)
			}
		})
	}
}

func TestClaudeExplorationActionsIncompleteAndUnknown(t *testing.T) {
	read := ClaudeExplorationActions("Read", map[string]any{"offset": 10})
	if read == nil || len(read.Actions) != 1 || string(read.Actions[0].Kind) != "read" || len(read.Actions[0].Items) != 0 {
		t.Fatalf("missing read path should produce incomplete read action, got %#v", read)
	}
	grep := ClaudeExplorationActions("Grep", map[string]any{"path": "internal"})
	if grep == nil || len(grep.Actions) != 1 || string(grep.Actions[0].Kind) != "search" || grep.Actions[0].Summary != "" || grep.Actions[0].Secondary != "internal" {
		t.Fatalf("missing grep pattern should produce incomplete search action with secondary, got %#v", grep)
	}
	if got := ClaudeExplorationActions("Bash", map[string]any{"command": "rg needle"}); got != nil {
		t.Fatalf("non exploration tool should not produce actions: %#v", got)
	}
	if got := ClaudeExplorationActions("SomethingElse", map[string]any{"pattern": "needle"}); got != nil {
		t.Fatalf("unknown tool should not produce actions: %#v", got)
	}
}

func TestIsInternalInteractionTool(t *testing.T) {
	cases := map[string]bool{
		"AskUserQuestion": true,
		"ExitPlanMode":    true,
		"Bash":            false,
		" Read ":          false,
	}
	for tool, want := range cases {
		if got := IsInternalInteractionTool(tool); got != want {
			t.Errorf("IsInternalInteractionTool(%q) = %v, want %v", tool, got, want)
		}
	}
}

func TestClaudeToolItemKind(t *testing.T) {
	cases := map[string]string{
		"Bash":          "command_execution",
		"WebSearch":     "web_search",
		"TodoWrite":     "",
		"Task":          "delegated_task",
		"TaskOutput":    "",
		"Edit":          "file_change",
		"Read":          "dynamic_tool_call",
		"SomethingElse": "dynamic_tool_call",
	}
	for tool, want := range cases {
		if got := ClaudeToolItemKind(tool); got != want {
			t.Errorf("ClaudeToolItemKind(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestClaudeDynamicToolSemanticKind(t *testing.T) {
	cases := map[string]string{
		"Read":  "exploration",
		"Skill": "skill",
		"Edit":  "file_change_request",
		"Other": "generic_tool",
	}
	for tool, want := range cases {
		if got := ClaudeDynamicToolSemanticKind(tool); got != want {
			t.Errorf("ClaudeDynamicToolSemanticKind(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestClaudeToolMetadata(t *testing.T) {
	metadata := ClaudeToolMetadata("Bash", map[string]any{
		"command": "ls -la",
		"cwd":     "/tmp",
		"extra":   "ignored",
	})
	if got := metadata["tool"]; got != "Bash" {
		t.Errorf("tool = %v, want Bash", got)
	}
	if got := metadata["command"]; got != "ls -la" {
		t.Errorf("command = %v, want ls -la", got)
	}
	if got := metadata["cwd"]; got != "/tmp" {
		t.Errorf("cwd = %v, want /tmp", got)
	}
	if _, ok := metadata["extra"]; ok {
		t.Errorf("extra should be filtered out, got %v", metadata)
	}
	args, ok := metadata["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("arguments missing: %v", metadata)
	}
	if args["command"] != "ls -la" {
		t.Errorf("arguments.command = %v", args["command"])
	}
	// input must be deep-cloned: mutating original must not affect metadata
	src := map[string]any{"command": "echo hi", "nested": map[string]any{"a": 1}}
	meta := ClaudeToolMetadata("Bash", src)
	nested := meta["arguments"].(map[string]any)["nested"].(map[string]any)
	nested["a"] = 99
	if src["nested"].(map[string]any)["a"] != 1 {
		t.Errorf("clone is shallow: original mutated")
	}
}

func TestMergeClaudeWebToolMetadata(t *testing.T) {
	meta := map[string]any{}
	MergeClaudeWebToolMetadata(meta, "WebSearch", map[string]any{"q": "hello"})
	if meta["actionType"] != "search" {
		t.Errorf("actionType = %v, want search", meta["actionType"])
	}
	if meta["query"] != "hello" {
		t.Errorf("query = %v, want hello (fallback to q)", meta["query"])
	}
}

func TestMergeClaudeFileChangeMetadataPayload(t *testing.T) {
	meta := map[string]any{}
	MergeClaudeFileChangeMetadata(meta, "Edit", map[string]any{
		"file_path":       "a.txt",
		"old_string":      "x",
		"new_string":      "y",
		"replace_all":     true,
		"structuredPatch": []map[string]any{{"old": "1"}},
	})
	if meta["semanticKind"] != "file_change_request" {
		t.Errorf("semanticKind = %v", meta["semanticKind"])
	}
	if meta["filePath"] != "a.txt" {
		t.Errorf("filePath = %v", meta["filePath"])
	}
	if meta["replaceAll"] != true {
		t.Errorf("replaceAll = %v", meta["replaceAll"])
	}
	records, ok := meta["structuredPatchRecords"].([]map[string]any)
	if !ok || len(records) != 1 {
		t.Errorf("structuredPatchRecords = %#v", meta["structuredPatchRecords"])
	}
}

func TestClaudeLookupBool(t *testing.T) {
	values := map[string]any{"replace_all": true}
	if got, ok := ClaudeLookupBool(values, "replaceAll", "replace_all"); !ok || !got {
		t.Errorf("ClaudeLookupBool = %v,%v want true,true", got, ok)
	}
	if _, ok := ClaudeLookupBool(map[string]any{}, "missing"); ok {
		t.Errorf("missing key should report not-ok")
	}
}

func TestBuildClaudeDelegatedTaskText(t *testing.T) {
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"description": "d", "subagentType": "s"}, "Task (s): d"},
		{map[string]any{"description": "d"}, "Task: d"},
		{map[string]any{"subagentType": "s"}, "Task (s)"},
		{map[string]any{"unknown": "x"}, "Task"},
		{map[string]any{}, ""},
	}
	for _, c := range cases {
		if got := BuildClaudeDelegatedTaskText(c.in); got != c.want {
			t.Errorf("BuildClaudeDelegatedTaskText(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToolUseSummary(t *testing.T) {
	if got := ToolUseSummary("Bash", map[string]any{"command": "ls"}); got != "ls" {
		t.Errorf("command summary = %q, want ls", got)
	}
	if got := ToolUseSummary("Bash", map[string]any{"description": "desc"}); got != "desc" {
		t.Errorf("description summary = %q, want desc", got)
	}
	if got := ToolUseSummary("Bash", map[string]any{"a": 1}); got != `{"a":1}` {
		t.Errorf("compact fallback = %q, want compact json", got)
	}
	if got := ToolUseSummary("Bash", map[string]any{}); got != "Bash" {
		t.Errorf("tool fallback = %q, want Bash", got)
	}
}

func TestClaudeHomeDir(t *testing.T) {
	t.Setenv("HOME", "/fake/home")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	if got := ClaudeHomeDir(); got != "/fake/home" {
		t.Errorf("ClaudeHomeDir = %q, want /fake/home", got)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", `C:\Users\fake`)
	if got := ClaudeHomeDir(); got != `C:\Users\fake` {
		t.Errorf("ClaudeHomeDir = %q, want C:\\Users\\fake", got)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "D:")
	t.Setenv("HOMEPATH", `\Users\fake`)
	if got := ClaudeHomeDir(); got != `D:\Users\fake` {
		t.Errorf("ClaudeHomeDir = %q, want D:\\Users\\fake", got)
	}
}

func TestMergeClaudeFileChangeMetadataPayloadNil(t *testing.T) {
	// nil metadata and empty payload must not panic
	MergeClaudeFileChangeMetadata(nil, "Edit", map[string]any{"x": 1})
	MergeClaudeFileChangeMetadataPayload(nil, "Edit", map[string]any{"x": 1})
	MergeClaudeFileChangeMetadataPayload(map[string]any{}, "Edit", nil)
}
