package daemon

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"strconv"
	"strings"
)

func parseTeamGoal(text string) (agentproto.CommandKind, agentproto.GoalCommand, error) {
	fields := strings.Fields(text)
	goal := agentproto.GoalCommand{Purpose: "user_control"}
	if len(fields) <= 1 || fields[1] == "status" {
		return agentproto.CommandThreadGoalGet, goal, nil
	}
	switch fields[1] {
	case "clear", "stop":
		if len(fields) != 2 {
			return "", goal, fmt.Errorf("用法：/goal clear")
		}
		return agentproto.CommandThreadGoalClear, goal, nil
	case "pause", "resume":
		if len(fields) != 2 {
			return "", goal, fmt.Errorf("用法：/goal pause 或 /goal resume")
		}
		goal.Status = "paused"
		if fields[1] == "resume" {
			goal.Status = "active"
		}
		return agentproto.CommandThreadGoalSet, goal, nil
	case "new", "edit", "set":
		objective := []string{}
		for index := 2; index < len(fields); index++ {
			word := fields[index]
			budget := ""
			if word == "--budget" {
				index++
				if index >= len(fields) {
					return "", goal, fmt.Errorf("--budget 后需要正整数")
				}
				budget = fields[index]
			} else if strings.HasPrefix(word, "--budget=") {
				budget = strings.TrimPrefix(word, "--budget=")
			}
			if budget != "" {
				if goal.TokenBudget != nil {
					return "", goal, fmt.Errorf("预算只能填写一次")
				}
				tokens, e := strconv.ParseInt(budget, 10, 64)
				if e != nil || tokens <= 0 {
					return "", goal, fmt.Errorf("会话预算必须是正整数")
				}
				goal.TokenBudget = &tokens
			} else {
				objective = append(objective, word)
			}
		}
		goal.Objective = strings.Join(objective, " ")
		if goal.Objective == "" {
			return "", goal, fmt.Errorf("请填写会话目标，例如 /goal new --budget 1200 完成当前登录流程")
		}
		return agentproto.CommandThreadGoalSet, goal, nil
	default:
		return "", goal, fmt.Errorf("用法：/goal、/goal new --budget 数量 目标、/goal edit、/goal pause、/goal resume 或 /goal clear")
	}
}
