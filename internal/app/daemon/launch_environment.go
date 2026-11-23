package daemon

import (
	"runtime"
	"sort"
	"strings"
)

// stableEffectiveLaunchEnvironment preserves the process's last-value-wins
// semantics while making map-derived launch material deterministic.
func stableEffectiveLaunchEnvironment(values []string) []string {
	entries := make(map[string]string, len(values))
	for _, entry := range values {
		key, _, ok := strings.Cut(entry, "=")
		if runtime.GOOS == "windows" && strings.HasPrefix(entry, "=") {
			if i := strings.Index(entry[1:], "="); i >= 0 {
				key = entry[:i+1]
				ok = true
			}
		}
		if !ok {
			key = entry
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		entries[key] = entry
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, entries[k])
	}
	return result
}
