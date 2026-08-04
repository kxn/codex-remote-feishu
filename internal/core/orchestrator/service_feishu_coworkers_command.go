package orchestrator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleCoworkersCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return notice(surface, "coworkers_group_required", "`/coworkers` 只能在群聊中设置或查看群内并发上限。")
	}
	if action.IsCardAction() {
		s.markCommandLauncherTerminal(surface)
	}
	argument, ok := coworkersCommandArgument(action.Text)
	if !ok {
		return notice(surface, "coworkers_invalid", "用法：`/coworkers N` 设置非负整数上限，`/coworkers status` 查看；0 表示不限制。")
	}
	if argument == "status" {
		return s.coworkersStatusNotice(surface, room)
	}
	if primaryBotStateForSurface(surface, room) != control.CatalogPrimaryBotStateCurrent {
		return notice(surface, "coworkers_primary_required", "请先对当前机器人执行 `/primary on`，再设置本群并发上限。")
	}
	limit, ok := parseCoworkersLimit(argument)
	if !ok {
		return notice(surface, "coworkers_invalid", "用法：`/coworkers N` 设置非负整数上限，`/coworkers status` 查看；0 表示不限制。")
	}
	room.ConcurrencyLimit = &limit
	return notice(surface, "coworkers_updated", fmt.Sprintf("已将本群机器人并发上限设置为 %s。当前 active 数量：%d。", formatCoworkersLimit(limit), s.feishuRoomActiveReservationCount(room)))
}

func (s *Service) coworkersStatusNotice(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord) []eventcontract.Event {
	limit := state.FeishuRoomConcurrencyLimit(room.ConcurrencyLimit)
	active := s.feishuRoomActiveReservationCount(room)
	return notice(surface, "coworkers_status", fmt.Sprintf("本群机器人当前 active 数量：%d；并发上限：%s。", active, formatCoworkersLimit(limit)))
}

func coworkersCommandArgument(text string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 1 {
		return "status", true
	}
	if len(fields) != 2 {
		return "", false
	}
	argument := strings.ToLower(strings.TrimSpace(fields[1]))
	if argument == "status" {
		return argument, true
	}
	if argument == "" {
		return "", false
	}
	for _, r := range argument {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return argument, true
}

func parseCoworkersLimit(argument string) (int, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(argument), 10, strconv.IntSize)
	if err != nil || value > uint64(maxIntValue()) {
		return 0, false
	}
	return int(value), true
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func formatCoworkersLimit(limit int) string {
	if limit == 0 {
		return "不限制（0）"
	}
	return strconv.Itoa(limit)
}
