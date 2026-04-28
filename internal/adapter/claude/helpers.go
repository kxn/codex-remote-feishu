package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const claudeLatestPlanGuessFreshness = 15 * time.Minute

func claudeHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home
	}
	if home := strings.TrimSpace(os.Getenv("USERPROFILE")); home != "" {
		return home
	}
	drive := strings.TrimSpace(os.Getenv("HOMEDRIVE"))
	path := strings.TrimSpace(os.Getenv("HOMEPATH"))
	if drive != "" && path != "" {
		return filepath.Clean(drive + path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = cloneJSONValue(value)
	}
	return output
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneJSONValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneJSONValue(item))
		}
		return cloned
	default:
		return value
	}
}

func lookupMap(value map[string]any, path ...string) map[string]any {
	current := any(value)
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		current = object[part]
	}
	object, _ := current.(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func lookupSliceMaps(value map[string]any, path ...string) []map[string]any {
	current := any(value)
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return mapsFromAny(current)
}

func mapsFromAny(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMap(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			object, _ := item.(map[string]any)
			if object != nil {
				out = append(out, cloneMap(object))
			}
		}
		return out
	default:
		return nil
	}
}

func lookupStringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func lookupBoolFromAny(value any) bool {
	current, _ := value.(bool)
	return current
}

func lookupIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func lookupStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if current := strings.TrimSpace(lookupStringFromAny(item)); current != "" {
				out = append(out, current)
			}
		}
		return out
	default:
		return nil
	}
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func marshalNDJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func stringifyTextContent(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var buffer bytes.Buffer
		for _, item := range typed {
			if buffer.Len() > 0 {
				buffer.WriteString("\n")
			}
			switch entry := item.(type) {
			case string:
				buffer.WriteString(entry)
			case map[string]any:
				if text := strings.TrimSpace(lookupStringFromAny(entry["text"])); text != "" {
					buffer.WriteString(text)
					continue
				}
				if text := strings.TrimSpace(lookupStringFromAny(entry["content"])); text != "" {
					buffer.WriteString(text)
				}
			}
		}
		return buffer.String()
	default:
		return ""
	}
}

func resolvePlanConfirmationRequestBody(assistantText string) (string, string, string) {
	if body := strings.TrimSpace(assistantText); body != "" {
		return body, "assistant_text", ""
	}
	return guessLatestClaudePlanBody()
}

func resolvePlanConfirmationResolvedBody(currentBody, currentSource, hintedPath string) (string, string, string) {
	currentBody = strings.TrimSpace(currentBody)
	currentSource = strings.TrimSpace(currentSource)
	if currentSource == "assistant_text" && currentBody != "" {
		return currentBody, currentSource, ""
	}
	if body, path, ok := readClaudePlanBodyFromPath(hintedPath); ok {
		return body, "tool_result.filePath", path
	}
	if currentBody != "" {
		return currentBody, currentSource, ""
	}
	return guessLatestClaudePlanBody()
}

func guessLatestClaudePlanBody() (string, string, string) {
	planPath, ok := guessLatestClaudePlanFilePath()
	if !ok {
		return "", "", ""
	}
	body, path, ok := readClaudePlanBodyFromPath(planPath)
	if !ok {
		return "", "", ""
	}
	return body, "latest_plan_file", path
}

func readClaudePlanBodyFromPath(rawPath string) (string, string, bool) {
	planPath := strings.TrimSpace(rawPath)
	if planPath == "" {
		return "", "", false
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		return "", "", false
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "", "", false
	}
	return body, planPath, true
}

func guessLatestClaudePlanFilePath() (string, bool) {
	homeDir := claudeHomeDir()
	if homeDir == "" {
		return "", false
	}
	candidates := []string{
		filepath.Join(homeDir, ".claude-all", "plans"),
		filepath.Join(homeDir, ".claude", "plans"),
	}
	var latestPath string
	var latestModTime time.Time
	for _, dir := range candidates {
		entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := os.Stat(entry)
			if err != nil || info.IsDir() {
				continue
			}
			if latestPath == "" || info.ModTime().After(latestModTime) {
				latestPath = entry
				latestModTime = info.ModTime()
			}
		}
	}
	if latestPath == "" {
		return "", false
	}
	if !latestModTime.IsZero() && time.Since(latestModTime) > claudeLatestPlanGuessFreshness {
		return "", false
	}
	return latestPath, true
}

func sanitizeQuestionID(value string, index int) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fmt.Sprintf("question_%d", index+1)
}

func buildQuestionMetadata(questions []agentproto.RequestQuestion) []map[string]any {
	if len(questions) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(questions))
	for _, question := range questions {
		record := map[string]any{
			"id":         strings.TrimSpace(question.ID),
			"header":     strings.TrimSpace(question.Header),
			"question":   strings.TrimSpace(question.Question),
			"allowOther": question.AllowOther,
			"secret":     question.Secret,
		}
		if strings.TrimSpace(question.Placeholder) != "" {
			record["placeholder"] = strings.TrimSpace(question.Placeholder)
		}
		if strings.TrimSpace(question.DefaultValue) != "" {
			record["defaultValue"] = strings.TrimSpace(question.DefaultValue)
		}
		if question.DirectResponse {
			record["directResponse"] = true
		}
		if len(question.Options) != 0 {
			options := make([]map[string]any, 0, len(question.Options))
			for _, option := range question.Options {
				options = append(options, map[string]any{
					"label":       strings.TrimSpace(option.Label),
					"description": strings.TrimSpace(option.Description),
				})
			}
			record["options"] = options
		}
		out = append(out, record)
	}
	return out
}

func buildClaudeTokenUsage(result map[string]any) *agentproto.ThreadTokenUsage {
	usageMap := lookupMap(result, "usage")
	if len(usageMap) == 0 {
		return nil
	}
	inputTokens := lookupIntFromAny(usageMap["input_tokens"])
	cacheReadTokens := lookupIntFromAny(usageMap["cache_read_input_tokens"])
	cacheCreateTokens := lookupIntFromAny(usageMap["cache_creation_input_tokens"])
	outputTokens := lookupIntFromAny(usageMap["output_tokens"])
	total := inputTokens + cacheReadTokens + cacheCreateTokens + outputTokens

	usage := &agentproto.ThreadTokenUsage{
		Total: agentproto.TokenUsageBreakdown{
			InputTokens:       inputTokens,
			CachedInputTokens: cacheReadTokens + cacheCreateTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       total,
		},
		Last: agentproto.TokenUsageBreakdown{
			InputTokens:       inputTokens,
			CachedInputTokens: cacheReadTokens + cacheCreateTokens,
			OutputTokens:      outputTokens,
			TotalTokens:       total,
		},
	}
	bestWindow := 0
	for _, modelUsage := range lookupMap(result, "modelUsage") {
		record, ok := modelUsage.(map[string]any)
		if !ok {
			continue
		}
		if current := lookupIntFromAny(record["contextWindow"]); current > bestWindow {
			bestWindow = current
		}
	}
	if bestWindow > 0 {
		usage.ModelContextWindow = &bestWindow
	}
	return usage
}
