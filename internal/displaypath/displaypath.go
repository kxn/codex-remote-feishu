package displaypath

import "strings"

func FileLabel(path string, labels map[string]string) string {
	normalized := Normalize(path)
	if normalized == "" {
		return ""
	}
	if label := strings.TrimSpace(labels[normalized]); label != "" {
		return label
	}
	return baseName(normalized)
}

func FileLabels(paths []string) map[string]string {
	unique := uniqueNormalized(paths)
	if len(unique) == 0 {
		return map[string]string{}
	}
	resolved := make(map[string]string, len(unique))
	maxDepth := 0
	for _, path := range unique {
		if depth := len(pathParts(path)); depth > maxDepth {
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
				resolved[path] = suffix
			}
		}
	}
	for _, path := range unique {
		if resolved[path] == "" {
			resolved[path] = path
		}
	}
	return resolved
}

func PathLabel(path string, labels map[string]string) string {
	normalized := Normalize(path)
	if normalized == "" {
		return ""
	}
	if label := strings.TrimSpace(labels[normalized]); label != "" {
		return label
	}
	return normalized
}

func PathLabels(paths []string) map[string]string {
	unique := uniqueNormalized(paths)
	if len(unique) == 0 {
		return map[string]string{}
	}
	resolved := make(map[string]string, len(unique))
	maxStage := 1
	for _, path := range unique {
		if stageCount := pathLabelStageCount(pathParts(path)); stageCount > maxStage {
			maxStage = stageCount
		}
	}
	for stage := 1; stage <= maxStage; stage++ {
		counts := map[string]int{}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			label := compactPathForStage(path, stage)
			counts[label]++
		}
		for _, path := range unique {
			if resolved[path] != "" {
				continue
			}
			label := compactPathForStage(path, stage)
			if counts[label] == 1 {
				resolved[path] = label
			}
		}
	}
	for _, path := range unique {
		if resolved[path] == "" {
			resolved[path] = path
		}
	}
	return resolved
}

func Normalize(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	return strings.Trim(path, "/")
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
	parts := pathParts(path)
	if depth >= len(parts) {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-depth:], "/")
}

func pathParts(path string) []string {
	normalized := Normalize(path)
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, "/")
}

func baseName(path string) string {
	parts := pathParts(path)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func pathLabelStageCount(parts []string) int {
	switch len(parts) {
	case 0:
		return 0
	case 1:
		return 1
	default:
		return len(parts) - 1
	}
}

func compactPathForStage(path string, stage int) string {
	parts := pathParts(path)
	if len(parts) == 0 {
		return ""
	}
	selected := map[int]bool{
		len(parts) - 1: true,
	}
	if len(parts) > 1 {
		selected[0] = true
	}
	if stage > 1 {
		for _, index := range remainingPathRevealOrder(parts) {
			if stage <= 1 {
				break
			}
			if selected[index] {
				continue
			}
			selected[index] = true
			stage--
		}
	}
	return joinSelectedPathParts(parts, selected)
}

func remainingPathRevealOrder(parts []string) []int {
	order := make([]int, 0, len(parts))
	left := 1
	right := len(parts) - 2
	for right >= left {
		order = append(order, right)
		if right == left {
			break
		}
		order = append(order, left)
		left++
		right--
	}
	return order
}

func joinSelectedPathParts(parts []string, selected map[int]bool) string {
	if len(parts) == 0 {
		return ""
	}
	out := make([]string, 0, len(parts))
	for index := 0; index < len(parts); {
		if selected[index] {
			out = append(out, parts[index])
			index++
			continue
		}
		if len(out) == 0 || out[len(out)-1] != "..." {
			out = append(out, "...")
		}
		for index < len(parts) && !selected[index] {
			index++
		}
	}
	return strings.Join(out, "/")
}
