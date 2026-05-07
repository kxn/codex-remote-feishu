package daemon

import (
	"errors"
	"strings"
)

type adminCommandMode string

const (
	adminCommandWeb          adminCommandMode = "web"
	adminCommandLocalWeb     adminCommandMode = "localweb"
	adminCommandAutostart    adminCommandMode = "autostart"
	adminCommandAutostartOn  adminCommandMode = "autostart_on"
	adminCommandAutostartOff adminCommandMode = "autostart_off"
)

type parsedAdminCommand struct {
	Mode adminCommandMode
}

func parseAdminCommandText(text string) (parsedAdminCommand, error) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) == 0 || fields[0] != "/admin" {
		return parsedAdminCommand{}, errors.New(adminCommandUsageSummary())
	}
	switch {
	case len(fields) == 2 && fields[1] == "web":
		return parsedAdminCommand{Mode: adminCommandWeb}, nil
	case len(fields) == 2 && fields[1] == "localweb":
		return parsedAdminCommand{Mode: adminCommandLocalWeb}, nil
	case len(fields) == 2 && fields[1] == "autostart":
		return parsedAdminCommand{Mode: adminCommandAutostart}, nil
	case len(fields) == 3 && fields[1] == "autostart" && fields[2] == "on":
		return parsedAdminCommand{Mode: adminCommandAutostartOn}, nil
	case len(fields) == 3 && fields[1] == "autostart" && fields[2] == "off":
		return parsedAdminCommand{Mode: adminCommandAutostartOff}, nil
	default:
		return parsedAdminCommand{}, errors.New(adminCommandUsageSummary())
	}
}

func adminCommandUsageSummary() string {
	return "请使用 `/admin`、`/admin web`、`/admin localweb`、`/admin autostart`、`/admin autostart on` 或 `/admin autostart off`。"
}
