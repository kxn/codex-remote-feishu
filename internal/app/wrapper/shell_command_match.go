package wrapper

import (
	"path"
	"strings"
	"unicode"
)

// Native UserShell items expose derive_exec_args formatted for display, e.g.
// /bin/bash -lc "cat '/private/payload'". Compare the decoded script argument
// exactly; neither a path substring nor a coincidental command prefix is proof.
func matchesPrivateShellCommand(display, expected string) bool {
	if display == expected {
		return display != ""
	}
	argv, ok := privateShellDisplayWords(display)
	if !ok || len(argv) < 3 {
		return false
	}
	shell := strings.ToLower(path.Base(strings.ReplaceAll(argv[0], "\\", "/")))
	switch shell {
	case "bash", "zsh", "sh":
		return len(argv) == 3 && (argv[1] == "-lc" || argv[1] == "-c") && argv[2] == expected
	case "cmd", "cmd.exe":
		return len(argv) == 3 && strings.EqualFold(argv[1], "/c") && argv[2] == expected
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		if len(argv) == 3 {
			return strings.EqualFold(argv[1], "-Command") && argv[2] == expected
		}
		return len(argv) == 4 && strings.EqualFold(argv[1], "-NoProfile") && strings.EqualFold(argv[2], "-Command") && argv[3] == expected
	}
	return false
}

// Parse only the literal argv display grammar emitted by shlex. Expansions,
// operators and malformed quoting are refused rather than interpreted.
func privateShellDisplayWords(value string) ([]string, bool) {
	var words []string
	var word strings.Builder
	quote := rune(0)
	started := false
	runes := []rune(value)
	if len(runes) > 32768 {
		return nil, false
	}
	for index := 0; index < len(runes); index++ {
		current := runes[index]
		if quote == '\'' {
			if current == '\'' {
				quote = 0
			} else {
				word.WriteRune(current)
			}
			continue
		}
		if current == '\\' {
			if index+1 >= len(runes) {
				return nil, false
			}
			next := runes[index+1]
			if quote == '"' && next != '\\' && next != '"' && next != '$' && next != '`' && next != '\n' {
				word.WriteRune(current)
				continue
			}
			word.WriteRune(next)
			index++
			started = true
			continue
		}
		if quote == '"' {
			if current == '"' {
				quote = 0
			} else if current == '$' || current == '`' {
				return nil, false
			} else {
				word.WriteRune(current)
			}
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			started = true
			continue
		}
		if unicode.IsSpace(current) {
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
				if len(words) > 16 {
					return nil, false
				}
			}
			continue
		}
		if strings.ContainsRune(";$`|&<>()", current) {
			return nil, false
		}
		word.WriteRune(current)
		started = true
	}
	if quote != 0 {
		return nil, false
	}
	if started {
		words = append(words, word.String())
	}
	return words, len(words) > 0
}
