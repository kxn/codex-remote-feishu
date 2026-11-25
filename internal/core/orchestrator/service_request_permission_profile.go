package orchestrator

import (
	"encoding/json"
	"fmt"
	"math"
)

// validCodexPermissionProfile recognizes the 0.171 profile without granting
// fields the UI cannot describe. Keep the original object for the response.
func validCodexPermissionProfile(profile map[string]any) bool {
	for key, value := range profile {
		if key != "network" && key != "fileSystem" {
			return false
		}
		if value == nil {
			continue
		}
		switch key {
		case "network":
			m, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for k, v := range m {
				if k != "enabled" {
					return false
				}
				if v != nil {
					if _, ok := v.(bool); !ok {
						return false
					}
				}
			}
		case "fileSystem":
			m, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for k, v := range m {
				if k != "read" && k != "write" && k != "entries" && k != "globScanMaxDepth" {
					return false
				}
				if v == nil {
					continue
				}
				switch k {
				case "read", "write":
					values, ok := v.([]any)
					if !ok {
						return false
					}
					for _, x := range values {
						if p, ok := x.(string); !ok || p == "" {
							return false
						}
					}
				case "globScanMaxDepth":
					n, ok := v.(float64)
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 9007199254740991 || math.Trunc(n) != n {
						return false
					}
				case "entries":
					values, ok := v.([]any)
					if !ok {
						return false
					}
					for _, x := range values {
						if !validCodexPermissionEntry(x) {
							return false
						}
					}
				default:
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
func validCodexPermissionEntry(value any) bool {
	m, ok := value.(map[string]any)
	if !ok || len(m) != 2 {
		return false
	}
	access, ok := m["access"].(string)
	if !ok || (access != "read" && access != "write" && access != "deny") {
		return false
	}
	path, ok := m["path"].(map[string]any)
	if !ok {
		return false
	}
	kind, _ := path["type"].(string)
	switch kind {
	case "path":
		p, ok := path["path"].(string)
		return ok && p != "" && len(path) == 2
	case "glob_pattern":
		p, ok := path["pattern"].(string)
		return ok && p != "" && len(path) == 2
	case "special":
		if len(path) != 2 {
			return false
		}
		special, ok := path["value"].(map[string]any)
		if !ok {
			return false
		}
		k, _ := special["kind"].(string)
		switch k {
		case "root", "minimal", "tmpdir", "slash_tmp":
			return len(special) == 1
		case "project_roots":
			if len(special) == 1 {
				return true
			}
			if len(special) != 2 {
				return false
			}
			v, ok := special["subpath"]
			if !ok {
				return false
			}
			if v == nil {
				return true
			}
			_, ok = v.(string)
			return ok
		default:
			return false
		}
	default:
		return false
	}
}
func codexPermissionProfileText(profile map[string]any) string {
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "无法读取权限范围"
	}
	return fmt.Sprintf("申请的文件与网络权限：\n%s", data)
}
