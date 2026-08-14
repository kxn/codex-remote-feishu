// Package pathcanon provides the cross-platform path canonicalization boundary
// for workspace/cwd handling. It separates three semantically different
// outputs that were previously produced from one slash-mixed string:
//
//   - Native: host-native path for cmd.Dir, --cwd, PowerShell / Task Scheduler,
//     and file IO. Windows extended-length prefixes ("\\?\" / "//?/") are
//     stripped because child processes and the Windows API reject them.
//   - WorkspaceKey: slash-form, extended-prefix-free key for state indexing and
//     cross-surface comparison.
//   - CompareKey: case-insensitive (on Windows-like paths) form of
//     WorkspaceKey for equality and containment checks.
//
// The ForGOOS variants take an explicit GOOS so Windows semantics can be unit
// tested on any host. The plain variants use runtime.GOOS.
package pathcanon

import (
	"path/filepath"
	"runtime"
	"strings"
)

// isWindowsGOOS reports whether goos denotes a Windows target.
func isWindowsGOOS(goos string) bool {
	return strings.EqualFold(strings.TrimSpace(goos), "windows")
}

// stripExtendedPrefix removes a Windows extended-length (verbatim) prefix from
// value. It recognizes both native ("\\?\") and slash ("//?/") forms, and maps
// extended UNC ("\\?\UNC\server\share" / "//?/UNC/server/share") back to plain
// UNC ("\\server\share" / "//server/share"). It is platform-independent and
// must run before any filepath-based cleaning so polluted inputs never reach
// state keys or process working directories.
func stripExtendedPrefix(value string) string {
	switch {
	case strings.HasPrefix(value, `\\?\UNC\`):
		return `\\` + value[len(`\\?\UNC\`):]
	case strings.HasPrefix(value, `//?/UNC/`):
		return `//` + value[len(`//?/UNC/`):]
	case strings.HasPrefix(value, `\\?\`):
		return value[len(`\\?\`):]
	case strings.HasPrefix(value, `//?/`):
		return value[len(`//?/`):]
	}
	return value
}

// isWindowsLikePath reports whether value looks like a Windows drive-letter or
// UNC path, regardless of the host GOOS. Used to apply case-insensitive
// comparison only to paths that are case-insensitive by Windows semantics.
func isWindowsLikePath(value string) bool {
	if len(value) >= 2 && value[1] == ':' {
		drive := value[0]
		return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
	}
	return strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`)
}

// splitWindowsPrefix splits a Windows path (separators already normalized to
// '/') into its volume prefix and the remaining path. prefix is "C:" for
// drive-letter paths, "//server/share" for UNC paths, or "" otherwise.
func splitWindowsPrefix(value string) (prefix, rest string) {
	if len(value) >= 2 && value[1] == ':' {
		drive := value[0]
		if (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z') {
			return value[:2], strings.TrimPrefix(value[2:], "/")
		}
	}
	if strings.HasPrefix(value, "//") {
		trimmed := value[2:]
		firstSep := strings.IndexByte(trimmed, '/')
		if firstSep > 0 {
			server := trimmed[:firstSep]
			restAll := trimmed[firstSep+1:]
			secondSep := strings.IndexByte(restAll, '/')
			if secondSep > 0 {
				share := restAll[:secondSep]
				if share != "" {
					return "//" + server + "/" + share, restAll[secondSep+1:]
				}
			} else if restAll != "" {
				return "//" + server + "/" + restAll, ""
			}
		}
	}
	return "", value
}

// cleanWindowsRest stack-cleans the path remainder (handles ".", "..", and
// empty segments) without crossing the volume prefix. Leading ".." segments
// are preserved so relative paths stay relative.
func cleanWindowsRest(rest string) string {
	parts := strings.Split(rest, "/")
	stack := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 && stack[len(stack)-1] != ".." {
				stack = stack[:len(stack)-1]
			} else {
				stack = append(stack, "..")
			}
		default:
			stack = append(stack, part)
		}
	}
	return strings.Join(stack, "/")
}

// windowsWorkspaceKey canonicalizes a Windows path to slash-form workspace key
// form: extended prefix stripped, volume prefix preserved, "." / ".." cleaned,
// and a leading root-relative separator preserved.
func windowsWorkspaceKey(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	leadingSlash := strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
	prefix, rest := splitWindowsPrefix(value)
	cleaned := cleanWindowsRest(rest)
	if prefix == "" {
		if leadingSlash && cleaned != "" && !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		return cleaned
	}
	if cleaned == "" {
		return prefix
	}
	return prefix + "/" + cleaned
}

// windowsNative canonicalizes a Windows path to native backslash form.
func windowsNative(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	leadingSlash := strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
	prefix, rest := splitWindowsPrefix(value)
	cleaned := cleanWindowsRest(rest)
	var builder strings.Builder
	if prefix != "" {
		builder.WriteString(strings.ReplaceAll(prefix, "/", `\`))
		if cleaned != "" {
			builder.WriteByte('\\')
		}
	}
	if cleaned != "" {
		if prefix == "" && leadingSlash {
			builder.WriteByte('\\')
		}
		builder.WriteString(strings.ReplaceAll(cleaned, "/", `\`))
	}
	return builder.String()
}

// NativeForGOOS returns the host-native form of path for the given GOOS.
func NativeForGOOS(goos, path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	value = stripExtendedPrefix(value)
	if isWindowsGOOS(goos) || isWindowsLikePath(value) {
		return windowsNative(value)
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

// Native returns the host-native form of path for runtime.GOOS.
func Native(path string) string {
	return NativeForGOOS(runtime.GOOS, path)
}

// WorkspaceKeyForGOOS returns the slash-form, extended-prefix-free key used
// for state indexing and cross-surface comparison on the given GOOS.
func WorkspaceKeyForGOOS(goos, path string) string {
	value := strings.TrimSpace(path)
	if value == "" {
		return ""
	}
	value = stripExtendedPrefix(value)
	if isWindowsGOOS(goos) || isWindowsLikePath(value) {
		key := windowsWorkspaceKey(value)
		if key == "." {
			return ""
		}
		return key
	}
	cleaned := filepath.Clean(value)
	if cleaned == "." {
		return ""
	}
	return filepath.ToSlash(cleaned)
}

// WorkspaceKey returns the slash-form workspace key for runtime.GOOS.
func WorkspaceKey(path string) string {
	return WorkspaceKeyForGOOS(runtime.GOOS, path)
}

// CompareKeyForGOOS returns a comparison key for the given GOOS: the
// WorkspaceKey lowercased when Windows semantics apply (Windows target or a
// Windows-like drive/UNC path), unchanged otherwise.
func CompareKeyForGOOS(goos, path string) string {
	key := WorkspaceKeyForGOOS(goos, path)
	if key == "" {
		return ""
	}
	if isWindowsGOOS(goos) || isWindowsLikePath(strings.TrimSpace(path)) {
		return strings.ToLower(key)
	}
	return key
}

// CompareKey returns the comparison key for runtime.GOOS.
func CompareKey(path string) string {
	return CompareKeyForGOOS(runtime.GOOS, path)
}

// ContainmentForGOOS reports whether target is root itself or a descendant of
// root on the given GOOS, using CompareKey semantics. The slash-normalized,
// case-folded prefix comparison is correct for both Windows (drive/UNC) and
// Unix paths and is independent of the host filepath behavior.
func ContainmentForGOOS(goos, root, target string) bool {
	rootKey := CompareKeyForGOOS(goos, root)
	targetKey := CompareKeyForGOOS(goos, target)
	if rootKey == "" || targetKey == "" {
		return false
	}
	return targetKey == rootKey || strings.HasPrefix(targetKey, rootKey+"/")
}

// Containment reports whether target is root itself or a descendant of root
// for runtime.GOOS.
func Containment(root, target string) bool {
	return ContainmentForGOOS(runtime.GOOS, root, target)
}

// IsWindowsLikePath reports whether path uses Windows drive-letter or UNC
// semantics, independent of the host GOOS.
func IsWindowsLikePath(path string) bool {
	return isWindowsLikePath(strings.TrimSpace(path))
}
