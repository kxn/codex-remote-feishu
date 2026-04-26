package displaypath

import "strings"

const maxLabelLen = 36

func DisplayLabel(path string, labels map[string]string) string {
	normalized := Normalize(path)
	if normalized == "" {
		return ""
	}
	if label := strings.TrimSpace(labels[normalized]); label != "" {
		return label
	}
	return Clamp(normalized)
}

func ShortestUniqueSuffixes(paths []string) map[string]string {
	unique := uniqueNormalized(paths)
	if len(unique) == 0 {
		return map[string]string{}
	}
	resolved := make(map[string]string, len(unique))
	maxDepth := 0
	for _, path := range unique {
		if depth := len(strings.Split(path, "/")); depth > maxDepth {
			maxDepth = depth
		}
	}
	for depth := 1; depth <= maxDepth; depth++ {
		counts := map[string]int{}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			counts[suffix]++
		}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			suffix := pathSuffix(path, depth)
			if counts[suffix] == 1 {
				resolved[path] = Clamp(suffix)
			}
		}
	}
	for _, path := range unique {
		if resolved[path] == "" {
			resolved[path] = Clamp(path)
		}
	}
	return resolved
}

func Normalize(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.Trim(path, "/")
}

func Clamp(path string) string {
	path = Normalize(path)
	if len(path) <= maxLabelLen {
		return path
	}
	if idx := strings.Index(path, "/"); idx >= 0 {
		tail := path
		if len(tail) > maxLabelLen-4 {
			tail = tail[len(tail)-(maxLabelLen-4):]
		}
		return ".../" + strings.TrimLeft(tail, "/")
	}
	return path[len(path)-maxLabelLen:]
}

func uniqueNormalized(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		normalized := Normalize(path)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func pathSuffix(path string, depth int) string {
	parts := strings.Split(Normalize(path), "/")
	if depth >= len(parts) {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}
