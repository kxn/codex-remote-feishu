package codexmanaged

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type version struct {
	text    string
	parts   [3]uint64
	preview string
}

func parseVersion(text string) (version, error) {
	match := versionPattern.FindStringSubmatch(text)
	if match == nil || len(text) > 128 {
		return version{}, fmt.Errorf("invalid Codex version %q", text)
	}
	parsed := version{text: text, preview: match[4]}
	for i := range parsed.parts {
		part, err := strconv.ParseUint(match[i+1], 10, 64)
		if err != nil {
			return version{}, fmt.Errorf("invalid Codex version %q", text)
		}
		parsed.parts[i] = part
	}
	for _, id := range strings.Split(parsed.preview, ".") {
		if len(id) > 1 && id[0] == '0' && numericIdentifier(id) {
			return version{}, fmt.Errorf("invalid Codex version %q", text)
		}
	}
	return parsed, nil
}

func numericIdentifier(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (v version) newerThan(other version) bool {
	for i, part := range v.parts {
		if part != other.parts[i] {
			return part > other.parts[i]
		}
	}
	if v.preview == other.preview {
		return false
	}
	if v.preview == "" {
		return true
	}
	if other.preview == "" {
		return false
	}
	left, right := strings.Split(v.preview, "."), strings.Split(other.preview, ".")
	for i := 0; i < len(left) && i < len(right); i++ {
		if left[i] == right[i] {
			continue
		}
		ln, rn := numericIdentifier(left[i]), numericIdentifier(right[i])
		if ln && rn {
			if len(left[i]) != len(right[i]) {
				return len(left[i]) > len(right[i])
			}
			return left[i] > right[i]
		}
		if ln != rn {
			return !ln
		}
		return left[i] > right[i]
	}
	return len(left) > len(right)
}
