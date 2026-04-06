package launcher

import "fmt"

type Role string

const (
	RoleHelp    Role = "help"
	RoleVersion Role = "version"
	RoleDaemon  Role = "daemon"
	RoleInstall Role = "install"
	RoleWrapper Role = "wrapper"
)

type Decision struct {
	Role Role
	Args []string
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
		if len(args) != 1 {
			return Decision{}, usageError("daemon does not accept extra arguments")
		}
		return Decision{Role: RoleDaemon}, nil
	case "install":
		return Decision{Role: RoleInstall, Args: args[1:]}, nil
	case "wrapper":
		if len(args) < 2 {
			return Decision{}, usageError("wrapper requires app-server arguments")
		}
		if args[1] != "app-server" {
			return Decision{}, usageError("wrapper only supports app-server mode")
		}
		return Decision{Role: RoleWrapper, Args: args[1:]}, nil
	case "app-server":
		return Decision{Role: RoleWrapper, Args: args}, nil
	default:
		return Decision{}, usageError(fmt.Sprintf("unsupported command: %s", args[0]))
	}
}

type usageError string

func (e usageError) Error() string {
	return string(e)
}
