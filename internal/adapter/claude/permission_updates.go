package claude

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	planPermissionScopeSession = "session"

	planPermissionGrantScopedRules              = "scoped_rules"
	planPermissionGrantSessionFileEdits         = "session_file_edits"
	planPermissionGrantSessionFileEditsAndFSOps = "session_file_edits_and_fs_ops"

	planPermissionRuleEditExistingFiles   = "edit_existing_files"
	planPermissionRuleCreateNewFiles      = "create_new_files"
	planPermissionRuleRenameOrMoveFiles   = "rename_or_move_files"
	planPermissionRuleDeletePlanFiles     = "delete_plan_files"
	planPermissionRuleRunCommonFSCommands = "run_common_fs_commands"

	planPermissionUpdateDestinationSession = "session"

	planPermissionNarrowedFeedback = "Session auto-grants were narrowed to path-scoped file edits/creates; move/delete/common filesystem commands still require approval when requested."
)

type planPermissionSelection struct {
	Scope       string
	GrantLevel  string
	Directories []string
	RuleClasses []string
}

type compiledPlanPermissionSelection struct {
	UpdatedPermissions []any
	FeedbackSuffix     string
}

func (t *Translator) planConfirmationUpdatedPermissions(request *pendingRequest, response map[string]any) ([]any, string, error) {
	if request == nil {
		return nil, "", nil
	}
	raw := lookupMap(response, "permissionSelection")
	if len(raw) == 0 {
		return nil, "", nil
	}
	selection, err := parsePlanPermissionSelection(raw)
	if err != nil {
		return nil, "", err
	}
	compiled := compilePlanPermissionSelection(selection, strings.TrimSpace(t.cwd))
	return compiled.UpdatedPermissions, compiled.FeedbackSuffix, nil
}

func parsePlanPermissionSelection(raw map[string]any) (planPermissionSelection, error) {
	selection := planPermissionSelection{
		Scope:       strings.TrimSpace(lookupStringFromAny(raw["scope"])),
		GrantLevel:  strings.TrimSpace(lookupStringFromAny(raw["grant_level"])),
		Directories: normalizedPlanPermissionValues(lookupStringList(raw["directories"])),
		RuleClasses: normalizedPlanPermissionValues(lookupStringList(raw["rule_classes"])),
	}
	if selection.Scope != planPermissionScopeSession {
		return planPermissionSelection{}, fmt.Errorf("unsupported plan permission scope %q", selection.Scope)
	}
	switch selection.GrantLevel {
	case planPermissionGrantScopedRules,
		planPermissionGrantSessionFileEdits,
		planPermissionGrantSessionFileEditsAndFSOps:
	default:
		return planPermissionSelection{}, fmt.Errorf("unsupported plan permission grant level %q", selection.GrantLevel)
	}
	if len(selection.Directories) == 0 {
		return planPermissionSelection{}, fmt.Errorf("plan permission selection requires at least one directory")
	}
	if len(selection.RuleClasses) == 0 {
		return planPermissionSelection{}, fmt.Errorf("plan permission selection requires at least one rule class")
	}
	return selection, nil
}

func normalizedPlanPermissionValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func compilePlanPermissionSelection(selection planPermissionSelection, cwd string) compiledPlanPermissionSelection {
	directories, wholeWorkspace := collapsePlanPermissionDirectories(selection.Directories, cwd)
	if shouldUsePlanPermissionAcceptEdits(selection, directories, wholeWorkspace, cwd) {
		updates := []any{map[string]any{
			"type":        "setMode",
			"mode":        claudePermissionModeAcceptEdits,
			"destination": planPermissionUpdateDestinationSession,
		}}
		if addDirs := buildPlanPermissionAddDirectories(directories, cwd); len(addDirs) != 0 {
			updates = append(updates, addDirs)
		}
		return compiledPlanPermissionSelection{UpdatedPermissions: updates}
	}

	rules := buildPlanPermissionRules(selection, directories, wholeWorkspace, cwd)
	updates := make([]any, 0, 2)
	if len(rules) != 0 {
		updates = append(updates, map[string]any{
			"type":        "addRules",
			"behavior":    "allow",
			"destination": planPermissionUpdateDestinationSession,
			"rules":       rules,
		})
	}
	if addDirs := buildPlanPermissionAddDirectories(directories, cwd); len(addDirs) != 0 {
		updates = append(updates, addDirs)
	}

	feedbackSuffix := ""
	if !wholeWorkspace && planPermissionSelectionHasAggressiveClasses(selection) {
		feedbackSuffix = planPermissionNarrowedFeedback
	}
	return compiledPlanPermissionSelection{
		UpdatedPermissions: updates,
		FeedbackSuffix:     feedbackSuffix,
	}
}

func collapsePlanPermissionDirectories(directories []string, cwd string) ([]string, bool) {
	if len(directories) == 0 {
		return nil, false
	}
	cwd = normalizedPlanPermissionPath(cwd)
	wholeWorkspace := false
	outside := make([]string, 0, len(directories))
	inside := make([]string, 0, len(directories))
	for _, dir := range directories {
		dir = normalizedPlanPermissionPath(dir)
		if dir == "" {
			continue
		}
		if cwd != "" && samePlanPermissionPath(dir, cwd) {
			wholeWorkspace = true
			continue
		}
		if cwd != "" && planPermissionPathWithin(dir, cwd) {
			inside = append(inside, dir)
			continue
		}
		outside = append(outside, dir)
	}
	if wholeWorkspace && cwd != "" {
		out := []string{cwd}
		out = append(out, outside...)
		return normalizedPlanPermissionValues(out), true
	}
	out := append([]string{}, inside...)
	out = append(out, outside...)
	return normalizedPlanPermissionValues(out), false
}

func shouldUsePlanPermissionAcceptEdits(selection planPermissionSelection, directories []string, wholeWorkspace bool, cwd string) bool {
	if !wholeWorkspace || cwd == "" {
		return false
	}
	if selection.GrantLevel != planPermissionGrantSessionFileEditsAndFSOps {
		return false
	}
	required := map[string]bool{
		planPermissionRuleEditExistingFiles:   true,
		planPermissionRuleCreateNewFiles:      true,
		planPermissionRuleRenameOrMoveFiles:   true,
		planPermissionRuleDeletePlanFiles:     true,
		planPermissionRuleRunCommonFSCommands: true,
	}
	for _, ruleClass := range selection.RuleClasses {
		delete(required, ruleClass)
	}
	return len(required) == 0
}

func buildPlanPermissionRules(selection planPermissionSelection, directories []string, wholeWorkspace bool, cwd string) []any {
	rules := make([]any, 0, len(directories)*4)
	hasRuleClass := map[string]bool{}
	for _, ruleClass := range selection.RuleClasses {
		hasRuleClass[ruleClass] = true
	}
	for _, dir := range directories {
		pattern := planPermissionRulePattern(dir, cwd)
		if pattern == "" {
			continue
		}
		if hasRuleClass[planPermissionRuleEditExistingFiles] {
			rules = append(rules, planPermissionRule("Edit", pattern))
		}
		if hasRuleClass[planPermissionRuleCreateNewFiles] {
			rules = append(rules, planPermissionRule("Write", pattern))
		}
	}
	if wholeWorkspace {
		if hasRuleClass[planPermissionRuleRenameOrMoveFiles] {
			rules = append(rules,
				planPermissionRule("Bash", "mv:*"),
				planPermissionRule("Bash", "cp:*"),
			)
		}
		if hasRuleClass[planPermissionRuleDeletePlanFiles] {
			rules = append(rules,
				planPermissionRule("Bash", "rm:*"),
				planPermissionRule("Bash", "rmdir:*"),
			)
		}
		if hasRuleClass[planPermissionRuleRunCommonFSCommands] {
			rules = append(rules,
				planPermissionRule("Bash", "mkdir:*"),
				planPermissionRule("Bash", "touch:*"),
				planPermissionRule("Bash", "sed:*"),
			)
		}
	}
	return dedupePlanPermissionRules(rules)
}

func buildPlanPermissionAddDirectories(directories []string, cwd string) map[string]any {
	additional := planPermissionAdditionalDirectories(directories, cwd)
	if len(additional) == 0 {
		return nil
	}
	out := make([]any, 0, len(additional))
	for _, dir := range additional {
		out = append(out, dir)
	}
	return map[string]any{
		"type":        "addDirectories",
		"destination": planPermissionUpdateDestinationSession,
		"directories": out,
	}
}

func planPermissionSelectionHasAggressiveClasses(selection planPermissionSelection) bool {
	for _, ruleClass := range selection.RuleClasses {
		switch ruleClass {
		case planPermissionRuleRenameOrMoveFiles,
			planPermissionRuleDeletePlanFiles,
			planPermissionRuleRunCommonFSCommands:
			return true
		}
	}
	return false
}

func planPermissionRule(toolName, ruleContent string) map[string]any {
	return map[string]any{
		"toolName":    toolName,
		"ruleContent": ruleContent,
	}
}

func dedupePlanPermissionRules(rules []any) []any {
	if len(rules) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]any, 0, len(rules))
	for _, raw := range rules {
		rule, _ := raw.(map[string]any)
		if len(rule) == 0 {
			continue
		}
		key := lookupStringFromAny(rule["toolName"]) + "\x00" + lookupStringFromAny(rule["ruleContent"])
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out
}

func planPermissionAdditionalDirectories(directories []string, cwd string) []string {
	if len(directories) == 0 {
		return nil
	}
	cwd = normalizedPlanPermissionPath(cwd)
	out := make([]string, 0, len(directories))
	for _, dir := range directories {
		dir = normalizedPlanPermissionPath(dir)
		if dir == "" {
			continue
		}
		if cwd != "" && (samePlanPermissionPath(dir, cwd) || planPermissionPathWithin(dir, cwd)) {
			continue
		}
		if !filepath.IsAbs(filepath.FromSlash(dir)) && cwd != "" {
			continue
		}
		out = append(out, dir)
	}
	return normalizedPlanPermissionValues(out)
}

func planPermissionRulePattern(directory, cwd string) string {
	directory = normalizedPlanPermissionPath(directory)
	if directory == "" {
		return ""
	}
	cwd = normalizedPlanPermissionPath(cwd)
	if cwd != "" {
		if rel, err := filepath.Rel(filepath.FromSlash(cwd), filepath.FromSlash(directory)); err == nil {
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return "./**"
			}
			if rel != ".." && !strings.HasPrefix(rel, "../") {
				return "./" + strings.TrimPrefix(rel, "./") + "/**"
			}
		}
	}
	if strings.HasPrefix(directory, "/") {
		return "//" + strings.TrimPrefix(directory, "/") + "/**"
	}
	return "./" + strings.TrimPrefix(directory, "./") + "/**"
}

func normalizedPlanPermissionPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
}

func samePlanPermissionPath(a, b string) bool {
	return normalizedPlanPermissionPath(a) == normalizedPlanPermissionPath(b)
}

func planPermissionPathWithin(pathValue, rootValue string) bool {
	pathValue = normalizedPlanPermissionPath(pathValue)
	rootValue = normalizedPlanPermissionPath(rootValue)
	if pathValue == "" || rootValue == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.FromSlash(rootValue), filepath.FromSlash(pathValue))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))
}
