package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestTeamNativeGoalParserPreservesExplicitSessionObjectiveAndBudget(t *testing.T) {
	kind, goal, e := parseTeamGoal("/goal new --budget 1200 完成登录流程")
	if e != nil || kind != agentproto.CommandThreadGoalSet || goal.Objective != "完成登录流程" || goal.TokenBudget == nil || *goal.TokenBudget != 1200 || goal.Purpose != "user_control" {
		t.Fatalf("goal %s %#v %v", kind, goal, e)
	}
	for _, text := range []string{"/goal new --budget -1 wrong", "/goal new --budget a wrong", "/goal new --budget 1 --budget 2 wrong", "/goal new"} {
		if _, _, e = parseTeamGoal(text); e == nil {
			t.Fatalf("invalid request accepted %s", text)
		}
	}
}
func TestTeamResultFilePathCannotEscapeExplicitRootsViaSymlink(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	if _, e := teamAuthorizedFilePath(foreign, "", []string{root}); e == nil {
		t.Fatal("unselected directory allowed")
	}
}
