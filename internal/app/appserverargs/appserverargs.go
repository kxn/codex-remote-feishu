package appserverargs

type Mode string

const (
	ModeCodex  Mode = "app-server"
	ModeClaude Mode = "claude-app-server"
)

type Match struct {
	Mode  Mode
	Index int
}

func IsMode(arg string) bool {
	switch Mode(arg) {
	case ModeCodex, ModeClaude:
		return true
	default:
		return false
	}
}

func Find(args []string) (Match, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if IsMode(arg) {
			return Match{Mode: Mode(arg), Index: i}, true
		}
		next, ok := skipKnownCodexRootOption(args, i)
		if !ok {
			return Match{}, false
		}
		i = next - 1
	}
	return Match{}, false
}

func skipKnownCodexRootOption(args []string, index int) (int, bool) {
	arg := args[index]
	switch arg {
	case "-c", "--config", "-C", "--cd":
		if index+1 >= len(args) {
			return 0, false
		}
		return index + 2, true
	default:
		if hasRootOptionValue(arg, "--config=") || hasRootOptionValue(arg, "--cd=") {
			return index + 1, true
		}
		return 0, false
	}
}

func hasRootOptionValue(arg, prefix string) bool {
	if len(arg) <= len(prefix) {
		return false
	}
	return arg[:len(prefix)] == prefix
}
