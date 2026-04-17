package feishu

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func previewRecordScopeKey(fileKey string) string {
	fileKey = strings.TrimSpace(fileKey)
	if fileKey == "" {
		return ""
	}
	parts := strings.SplitN(fileKey, "|", 2)
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func previewPermissionDrift(ctx context.Context, api previewDriveAPI, token, docType string, expected map[string]bool) (bool, error) {
	if api == nil || len(expected) == 0 {
		return false, nil
	}
	actual, err := api.ListPermissionMembers(ctx, token, docType)
	if err != nil {
		return false, err
	}
	for key, wanted := range expected {
		if !wanted {
			continue
		}
		if !actual[key] {
			return true, nil
		}
	}
	return false, nil
}

func clearPreviewScope(state *previewState, scopeKey string) {
	if state == nil {
		return
	}
	delete(state.Scopes, scopeKey)
	for key, record := range state.Files {
		if record == nil {
			delete(state.Files, key)
			continue
		}
		if strings.HasPrefix(key, scopeKey+"|") {
			delete(state.Files, key)
		}
	}
}

func ensurePreviewPermissions(ctx context.Context, api previewDriveAPI, token, docType string, shared *map[string]bool, principals []previewPrincipal) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing preview token for %s", docType)
	}
	if len(principals) == 0 {
		return nil
	}
	if *shared == nil {
		*shared = map[string]bool{}
	}

	available := 0
	var errs []string
	var firstErr error
	for _, principal := range principals {
		if principal.Key == "" || principal.MemberType == "" || principal.MemberID == "" || principal.Type == "" {
			continue
		}
		if (*shared)[principal.Key] {
			available++
			continue
		}
		if err := api.GrantPermission(ctx, token, docType, principal); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			errs = append(errs, fmt.Sprintf("%s => %v", principal.Key, err))
			continue
		}
		(*shared)[principal.Key] = true
		available++
	}
	if available > 0 {
		return nil
	}
	if firstErr != nil && isPreviewResourceMissingError(firstErr) {
		return firstErr
	}
	if len(errs) == 0 {
		return fmt.Errorf("no preview principals available")
	}
	return errors.New(strings.Join(errs, "; "))
}

func previewPrincipals(surfaceSessionID, chatID, actorUserID string) []previewPrincipal {
	seen := map[string]bool{}
	values := []previewPrincipal{}

	if userPrincipal, ok := previewUserPrincipal(actorUserID); ok {
		seen[userPrincipal.Key] = true
		values = append(values, userPrincipal)
	}
	if ref, ok := ParseSurfaceRef(surfaceSessionID); ok && ref.ScopeKind == ScopeKindChat {
		if chatPrincipal, ok := previewChatPrincipal(chatID); ok && !seen[chatPrincipal.Key] {
			seen[chatPrincipal.Key] = true
			values = append(values, chatPrincipal)
		}
	}
	return values
}

func previewUserPrincipal(actorUserID string) (previewPrincipal, bool) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return previewPrincipal{}, false
	}
	memberType := "userid"
	switch {
	case strings.HasPrefix(actorUserID, "ou_"):
		memberType = "openid"
	case strings.HasPrefix(actorUserID, "on_"):
		memberType = "unionid"
	}
	return previewPrincipal{
		Key:        memberType + ":" + actorUserID,
		MemberType: memberType,
		MemberID:   actorUserID,
		Type:       "user",
	}, true
}

func previewChatPrincipal(chatID string) (previewPrincipal, bool) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return previewPrincipal{}, false
	}
	return previewPrincipal{
		Key:        "openchat:" + chatID,
		MemberType: "openchat",
		MemberID:   chatID,
		Type:       "chat",
	}, true
}

func previewScopeKey(gatewayID, surfaceSessionID, chatID, actorUserID string) string {
	if strings.TrimSpace(surfaceSessionID) != "" {
		return surfaceSessionID
	}
	gatewayID = normalizeGatewayID(gatewayID)
	if strings.TrimSpace(chatID) != "" {
		return SurfaceRef{
			Platform:  PlatformFeishu,
			GatewayID: gatewayID,
			ScopeKind: ScopeKindChat,
			ScopeID:   strings.TrimSpace(chatID),
		}.SurfaceID()
	}
	return SurfaceRef{
		Platform:  PlatformFeishu,
		GatewayID: gatewayID,
		ScopeKind: ScopeKindUser,
		ScopeID:   strings.TrimSpace(actorUserID),
	}.SurfaceID()
}

func previewScopeFolderName(scopeKey string) string {
	name := strings.NewReplacer(":", "-", "/", "-", "\\", "-", " ", "-").Replace(strings.TrimSpace(scopeKey))
	name = strings.Trim(name, "-")
	if name == "" {
		name = "feishu-preview-scope"
	}
	return limitNameBytes(name, 120)
}

func previewFileKey(scopeKey, resolvedPath, contentSHA string) string {
	return scopeKey + "|" + resolvedPath + "|" + contentSHA
}

func previewFileName(resolvedPath, contentSHA string) string {
	base := filepath.Base(resolvedPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	shortSHA := contentSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}
	name = limitNameBytes(name, 200-len(ext)-len(shortSHA)-2)
	if name == "" {
		name = "preview"
	}
	return name + "--" + shortSHA + ext
}

func previewArtifactMetadata(path string) (artifactKind string, mimeType string, ok bool) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".md":
		return "markdown", "text/markdown", true
	case ".html", ".htm":
		return "html", "text/html", true
	case ".png":
		return "image", "image/png", true
	case ".jpg", ".jpeg":
		return "image", "image/jpeg", true
	case ".gif":
		return "image", "image/gif", true
	case ".webp":
		return "image", "image/webp", true
	case ".svg":
		return "svg", "image/svg+xml", true
	case ".pdf":
		return "pdf", "application/pdf", true
	case ".txt", ".log", ".json", ".yaml", ".yml", ".xml", ".csv", ".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".sh", ".sql", ".ini", ".toml", ".diff", ".patch":
		return "text", "text/plain; charset=utf-8", true
	default:
		mimeType = strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(strings.TrimSpace(path)))))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return "binary", mimeType, true
	}
}

func isDrivePreferredPreviewArtifactKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "markdown":
		return true
	default:
		return false
	}
}

func limitNameBytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	for len(value) > limit {
		value = value[:len(value)-1]
	}
	return strings.TrimRight(value, "-_.")
}

var previewHashLocationSuffixPattern = regexp.MustCompile(`^#L(\d+)(?:C(\d+))?$`)
var previewLocationNumberPattern = regexp.MustCompile(`^\d+(?::\d+)?$`)

func stripMarkdownLocationSuffix(target string) (string, string) {
	base, _, suffix := splitPreviewLocationSuffix(target)
	return base, suffix
}

func splitPreviewLocationSuffix(target string) (base string, location PreviewLocation, suffix string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", PreviewLocation{}, ""
	}
	if idx := strings.Index(target, "#"); idx >= 0 {
		candidateBase := target[:idx]
		candidateSuffix := target[idx:]
		if matched := previewHashLocationSuffixPattern.FindStringSubmatch(candidateSuffix); len(matched) == 3 {
			location = PreviewLocation{
				Line:   parsePreviewLocationNumber(matched[1]),
				Column: parsePreviewLocationNumber(matched[2]),
			}
			if location.Valid() {
				return candidateBase, location, candidateSuffix
			}
		}
	}
	if matched := previewColonLocationSuffixPattern.FindStringSubmatch(target); len(matched) == 3 && previewLocationNumberPattern.MatchString(matched[2][1:]) {
		base = matched[1]
		suffix = matched[2]
		line, column := parsePreviewColonLocationSuffix(suffix)
		location = PreviewLocation{Line: line, Column: column}
		if location.Valid() {
			return base, location, suffix
		}
	}
	return target, PreviewLocation{}, ""
}

func parsePreviewColonLocationSuffix(suffix string) (line int, column int) {
	suffix = strings.TrimPrefix(strings.TrimSpace(suffix), ":")
	if suffix == "" {
		return 0, 0
	}
	parts := strings.SplitN(suffix, ":", 3)
	if len(parts) > 0 {
		line = parsePreviewLocationNumber(parts[0])
	}
	if len(parts) > 1 {
		column = parsePreviewLocationNumber(parts[1])
	}
	return line, column
}

func parsePreviewLocationNumber(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	value := 0
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return 0
		}
		value = value*10 + int(ch-'0')
	}
	return value
}

func previewAllowedRoots(values ...string) []string {
	seen := map[string]bool{}
	roots := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		resolved, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(resolved); err == nil {
			resolved = real
		}
		resolved = filepath.Clean(resolved)
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		roots = append(roots, resolved)
	}
	return roots
}

func previewPathCandidates(target string, roots []string) []string {
	if absolute, ok := previewLexicalAbsolutePath(target); ok {
		return []string{absolute}
	}
	candidates := make([]string, 0, len(roots))
	for _, root := range roots {
		candidates = append(candidates, filepath.Join(root, target))
	}
	return candidates
}

func previewLexicalAbsolutePath(target string) (string, bool) {
	if len(target) >= 4 && isPreviewPathSeparator(target[0]) && isASCIILetter(target[1]) && target[2] == ':' && isPreviewPathSeparator(target[3]) {
		return target[1:], true
	}
	if filepath.IsAbs(target) {
		return target, true
	}
	if len(target) >= 3 && isASCIILetter(target[0]) && target[1] == ':' && isPreviewPathSeparator(target[2]) {
		return target, true
	}
	return "", false
}

func isPreviewPathSeparator(ch byte) bool {
	return ch == '/' || ch == '\\'
}

func isASCIILetter(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z')
}

func previewCanonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if os.IsNotExist(err) {
		return "", err
	}
	return absolute, nil
}

func previewPathWithinAnyRoot(path string, roots []string) bool {
	if len(roots) == 0 {
		return false
	}
	for _, root := range roots {
		if previewPathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func previewPathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isPreviewResourceMissingError(err error) bool {
	var apiErr *driveAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case 1061003, 1061007, 1061041, 1061044, 1063005:
		return true
	default:
		return false
	}
}

func isPreviewParentMissingError(err error) bool {
	var apiErr *driveAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case 1061003, 1061007, 1061041, 1061044:
		return true
	default:
		return false
	}
}

func isPreviewDriveAccessDeniedError(err error) bool {
	var apiErr *driveAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case 99991672:
		return true
	default:
		return false
	}
}
