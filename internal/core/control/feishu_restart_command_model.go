package control

import (
	"fmt"
	"strings"
)

type RestartCommandMode string

const (
	RestartCommandShowStatus RestartCommandMode = "status"
	RestartCommandChild      RestartCommandMode = "child"
)

type ParsedRestartCommand struct {
	Mode RestartCommandMode
}

type RestartCommandPresentation string

const (
	RestartCommandPresentationPage    RestartCommandPresentation = "page"
	RestartCommandPresentationExecute RestartCommandPresentation = "execute"
)

func ParseFeishuRestartCommandText(text string) (ParsedRestartCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedRestartCommand{}, fmt.Errorf("缺少 /restart 子命令。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/restart" {
		return ParsedRestartCommand{}, fmt.Errorf("不支持的 /restart 子命令。")
	}
	switch len(fields) {
	case 1:
		return ParsedRestartCommand{Mode: RestartCommandShowStatus}, nil
	case 2:
		if fields[1] == "child" {
			return ParsedRestartCommand{Mode: RestartCommandChild}, nil
		}
		return ParsedRestartCommand{}, fmt.Errorf("不支持的 /restart 子命令。")
	default:
		return ParsedRestartCommand{}, fmt.Errorf("不支持的 /restart 子命令。")
	}
}

func FeishuRestartCommandPresentationForText(text string) (RestartCommandPresentation, bool) {
	parsed, err := ParseFeishuRestartCommandText(text)
	if err != nil {
		return "", false
	}
	switch parsed.Mode {
	case RestartCommandShowStatus:
		return RestartCommandPresentationPage, true
	case RestartCommandChild:
		return RestartCommandPresentationExecute, true
	default:
		return "", false
	}
}

func FeishuRestartCommandRunsImmediately(text string) bool {
	presentation, ok := FeishuRestartCommandPresentationForText(text)
	return ok && presentation == RestartCommandPresentationExecute
}
