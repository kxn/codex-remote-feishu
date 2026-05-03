package control

import (
	"fmt"
	"strings"
)

type RepairCommandMode string

const (
	RepairCommandDefault RepairCommandMode = "default"
	RepairCommandDaemon  RepairCommandMode = "daemon"
)

type ParsedRepairCommand struct {
	Mode RepairCommandMode
}

func ParseFeishuRepairCommandText(text string) (ParsedRepairCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedRepairCommand{}, fmt.Errorf("缺少 /repair 子命令。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/repair" {
		return ParsedRepairCommand{}, fmt.Errorf("不支持的 /repair 子命令。")
	}
	switch len(fields) {
	case 1:
		return ParsedRepairCommand{Mode: RepairCommandDefault}, nil
	case 2:
		if fields[1] == "daemon" {
			return ParsedRepairCommand{Mode: RepairCommandDaemon}, nil
		}
		return ParsedRepairCommand{}, fmt.Errorf("不支持的 /repair 子命令。")
	default:
		return ParsedRepairCommand{}, fmt.Errorf("不支持的 /repair 子命令。")
	}
}
