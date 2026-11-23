package preview

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	turnDiffAnchorSearchMaxDepth = 2
	turnDiffAnchorSearchMaxDirs  = 192
)

type turnDiffAnchorLookup struct {
	Target         string
	DirectRoots    []string
	FallbackRoots  []string
	ExpectedBlobID string
}

type turnDiffAnchorCandidate struct {
	ResolvedPath string
	Content      []byte
}

func (p *DriveMarkdownPreviewer) loadTurnDiffAnchorText(file turnDiffParsedFile, req FinalBlockPreviewRequest) (string, bool, bool) {
	directRoots := p.requestRoots(req)
	if len(directRoots) == 0 {
		return "", false, false
	}
	fallbackRoots := previewAllowedRoots(req.ThreadCWD, req.WorkspaceRoot)
	if len(fallbackRoots) == 0 {
		fallbackRoots = directRoots
	}
	if text, ok := p.readTurnDiffPathText(turnDiffAnchorLookup{
		Target:         file.NewPath,
		DirectRoots:    directRoots,
		FallbackRoots:  fallbackRoots,
		ExpectedBlobID: file.NewBlobID,
	}); ok {
		return text, true, true
	}
	if text, ok := p.readTurnDiffPathText(turnDiffAnchorLookup{
		Target:         file.OldPath,
		DirectRoots:    directRoots,
		FallbackRoots:  fallbackRoots,
		ExpectedBlobID: file.OldBlobID,
	}); ok {
		return text, false, true
	}
	return "", false, false
}

func (p *DriveMarkdownPreviewer) readTurnDiffPathText(lookup turnDiffAnchorLookup) (string, bool) {
	target := strings.TrimSpace(lookup.Target)
	if target == "" {
		return "", false
	}
	directRoots := previewAllowedRoots(lookup.DirectRoots...)
	if len(directRoots) == 0 {
		return "", false
	}
	expectedBlobID := normalizeTurnDiffExpectedBlobID(lookup.ExpectedBlobID)
	if candidate, ok := collectSingleTurnDiffAnchorCandidate(
		target,
		directRoots,
		directRoots,
		expectedBlobID,
	); ok {
		return string(candidate.Content), true
	}
	if _, absolute := previewLexicalAbsolutePath(target); absolute {
		return "", false
	}
	fallbackRoots := previewAllowedRoots(lookup.FallbackRoots...)
	if len(fallbackRoots) == 0 {
		fallbackRoots = directRoots
	}
	descendantRoots := turnDiffDescendantRoots(fallbackRoots, turnDiffAnchorSearchMaxDepth, turnDiffAnchorSearchMaxDirs)
	if len(descendantRoots) == 0 {
		return "", false
	}
	if candidate, ok := collectSingleTurnDiffAnchorCandidate(
		target,
		descendantRoots,
		fallbackRoots,
		expectedBlobID,
	); ok {
		return string(candidate.Content), true
	}
	return "", false
}

func collectSingleTurnDiffAnchorCandidate(target string, candidateRoots []string, allowedRoots []string, expectedBlobID string) (turnDiffAnchorCandidate, bool) {
	paths := previewPathCandidates(target, candidateRoots)
	candidates := collectTurnDiffAnchorCandidates(paths, allowedRoots, expectedBlobID)
	if len(candidates) != 1 {
		return turnDiffAnchorCandidate{}, false
	}
	return candidates[0], true
}

func collectTurnDiffAnchorCandidates(paths []string, allowedRoots []string, expectedBlobID string) []turnDiffAnchorCandidate {
	seen := map[string]bool{}
	candidates := make([]turnDiffAnchorCandidate, 0, len(paths))
	for _, path := range paths {
		resolved, err := previewCanonicalPath(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		if !previewPathWithinAnyRoot(resolved, allowedRoots) {
			continue
		}
		if seen[resolved] {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			continue
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		if expectedBlobID != "" {
			actualBlobID := turnDiffGitBlobHash(content)
			if !strings.HasPrefix(actualBlobID, expectedBlobID) {
				continue
			}
		}
		seen[resolved] = true
		candidates = append(candidates, turnDiffAnchorCandidate{
			ResolvedPath: resolved,
			Content:      content,
		})
	}
	return candidates
}

func turnDiffDescendantRoots(roots []string, maxDepth, maxDirs int) []string {
	if len(roots) == 0 || maxDepth <= 0 || maxDirs <= 0 {
		return nil
	}
	type queueEntry struct {
		path  string
		depth int
	}
	seen := map[string]bool{}
	queue := make([]queueEntry, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		queue = append(queue, queueEntry{path: root, depth: 0})
	}

	result := make([]string, 0, maxDirs)
	for len(queue) > 0 && len(result) < maxDirs {
		current := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := strings.TrimSpace(entry.Name())
			if skipTurnDiffDescendantDir(name) {
				continue
			}
			child := filepath.Clean(filepath.Join(current.path, name))
			if child == "" || seen[child] {
				continue
			}
			seen[child] = true
			result = append(result, child)
			if current.depth+1 < maxDepth {
				queue = append(queue, queueEntry{path: child, depth: current.depth + 1})
			}
			if len(result) >= maxDirs {
				break
			}
		}
	}
	return result
}

func skipTurnDiffDescendantDir(name string) bool {
	switch name {
	case "", ".", "..", ".git", ".svn", ".hg", "node_modules", ".yarn", ".pnpm-store":
		return true
	default:
		return false
	}
}

func normalizeTurnDiffExpectedBlobID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	allZero := true
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
		if ch != '0' {
			allZero = false
		}
	}
	if allZero {
		return ""
	}
	return value
}

func turnDiffGitBlobHash(content []byte) string {
	header := fmt.Sprintf("blob %d%c", len(content), 0)
	hasher := sha1.New()
	_, _ = hasher.Write([]byte(header))
	_, _ = hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil))
}
