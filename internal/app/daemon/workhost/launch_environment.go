package workhost

import (
	"reflect"
	"runtime"
	"strings"
)

// Environment order is not semantic; the final value for each key is. Some
// profile material is produced from maps, so comparing slices causes random
// false conflicts while accepting a changed final value would be unsafe.
func equalLaunchEnvironment(a, b []string) bool {
	normalize := func(values []string) map[string]string {
		m := make(map[string]string, len(values))
		for _, v := range values {
			k, value, ok := strings.Cut(v, "=")
			if runtime.GOOS == "windows" && strings.HasPrefix(v, "=") {
				if i := strings.Index(v[1:], "="); i >= 0 {
					k = v[:i+1]
					value = v[i+2:]
					ok = true
				}
			}
			if !ok {
				k = v
				value = ""
			}
			if runtime.GOOS == "windows" {
				k = strings.ToUpper(k)
			}
			m[k] = value
		}
		return m
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}
