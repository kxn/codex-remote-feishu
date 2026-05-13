package launcher

import "fmt"

type Role string

const (
	RoleHelp                 Role = "help"
	RoleVersion              Role = "version"
	RoleDaemon               Role = "daemon"
	RoleInstall              Role = "install"
	RolePackagedInstall      Role = "packaged_install"
	RolePackagedInstallProbe Role = "packaged_install_probe"
	RoleLocalUpgrade         Role = "local_upgrade"
	RoleService              Role = "service"
	RoleUpgradeHelper        Role = "upgrade_helper"
	RoleWrapper              Role = "wrapper"
)

type Decision struct {
	Role Role
	Args []string
}

func isWrapperMode(mode string) bool {
	switch mode {
	case "app-server", "claude-app-server":
		return true
	default:
		return false
	}
}

func Detect(args []string) (Decision, error) {
	if len(args) == 0 {
		return Decision{Role: RoleDaemon}, nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		return Decision{Role: RoleHelp}, nil
	case "version", "--version":
		return Decision{Role: RoleVersion}, nil
	case "daemon":
		return Decision{Role: RoleDaemon, Args: args[1:]}, nil
	case "install":
		return Decision{Role: RoleInstall, Args: args[1:]}, nil
	case "packaged-install":
		return Decision{Role: RolePackagedInstall, Args: args[1:]}, nil
	case "packaged-install-probe":
		return Decision{Role: RolePackagedInstallProbe, Args: args[1:]}, nil
	case "local-upgrade":
		return Decision{Role: RoleLocalUpgrade, Args: args[1:]}, nil
	case "service":
		return Decision{Role: RoleService, Args: args[1:]}, nil
	case "upgrade-helper":
		return Decision{Role: RoleUpgradeHelper, Args: args[1:]}, nil
	case "wrapper":
		if len(args) < 2 {
			return Decision{}, usageError("wrapper requires app-server or claude-app-server arguments")
		}
		if !isWrapperMode(args[1]) {
			return Decision{}, usageError("wrapper only supports app-server or claude-app-server mode")
		}
		return Decision{Role: RoleWrapper, Args: args[1:]}, nil
	case "app-server", "claude-app-server":
		return Decision{Role: RoleWrapper, Args: args}, nil
	default:
		return Decision{}, usageError(fmt.Sprintf("unsupported command: %s", args[0]))
	}
}

type usageError string

func (e usageError) Error() string {
	return string(e)
}
