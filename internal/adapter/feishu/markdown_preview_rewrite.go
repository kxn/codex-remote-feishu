package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func (p *DriveMarkdownPreviewer) RewriteFinalBlock(ctx context.Context, req MarkdownPreviewRequest) (render.Block, error) {
	block := req.Block
	if p == nil || p.api == nil {
		return block, nil
	}
	if !block.Final || block.Kind != render.BlockAssistantMarkdown || strings.TrimSpace(block.Text) == "" {
		return block, nil
	}

	principals := previewPrincipals(req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	if len(principals) == 0 {
		return block, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	rewritten, changed, rewriteErr := p.rewriteMarkdownLinksLocked(ctx, state, req, principals)
	if changed {
		block.Text = rewritten
		if err := p.saveStateLocked(); err != nil && rewriteErr == nil {
			rewriteErr = err
		}
	}
	return block, rewriteErr
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinksLocked(ctx context.Context, state *previewState, req MarkdownPreviewRequest, principals []previewPrincipal) (string, bool, error) {
	text := req.Block.Text
	matches := markdownLinkPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, false, nil
	}

	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	rewrittenTargets := map[string]string{}
	var errs []string

	var builder strings.Builder
	last := 0
	changed := false
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		targetStart := match[2]
		targetEnd := match[3]
		rawTarget := text[targetStart:targetEnd]

		builder.WriteString(text[last:targetStart])
		replacement := rawTarget
		if cached, ok := rewrittenTargets[rawTarget]; ok {
			replacement = cached
			if replacement != rawTarget {
				changed = true
			}
		} else {
			url, ok, err := p.materializeMarkdownTargetLocked(ctx, state, rawTarget, req, scopeKey, principals)
			switch {
			case err != nil:
				errs = append(errs, err.Error())
			case ok && url != "":
				replacement = url
				rewrittenTargets[rawTarget] = url
				changed = true
			default:
				rewrittenTargets[rawTarget] = rawTarget
			}
		}
		builder.WriteString(replacement)
		last = targetEnd
	}
	builder.WriteString(text[last:])

	var rewriteErr error
	if len(errs) > 0 {
		rewriteErr = errors.New(strings.Join(errs, "; "))
	}
	return builder.String(), changed, rewriteErr
}

func (p *DriveMarkdownPreviewer) materializeMarkdownTargetLocked(ctx context.Context, state *previewState, rawTarget string, req MarkdownPreviewRequest, scopeKey string, principals []previewPrincipal) (string, bool, error) {
	resolvedPath, ok, err := p.resolveMarkdownPath(rawTarget, req)
	if err != nil || !ok {
		return "", ok, err
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", true, fmt.Errorf("read markdown preview source %s: %w", resolvedPath, err)
	}
	if len(content) == 0 {
		return "", true, fmt.Errorf("skip empty markdown preview source %s", resolvedPath)
	}
	if int64(len(content)) > p.config.MaxFileBytes {
		return "", true, fmt.Errorf("markdown preview source exceeds %d bytes: %s", p.config.MaxFileBytes, resolvedPath)
	}

	sum := sha256.Sum256(content)
	contentSHA := hex.EncodeToString(sum[:])
	fileKey := previewFileKey(scopeKey, resolvedPath, contentSHA)

	scopeFolder, err := p.ensureScopeFolderLocked(ctx, state, scopeKey, principals)
	if err != nil {
		return "", true, err
	}

	record := state.Files[fileKey]
	if record == nil {
		record = &previewFileRecord{
			Path:      resolvedPath,
			SHA256:    contentSHA,
			ScopeKey:  scopeKey,
			SizeBytes: int64(len(content)),
		}
		state.Files[fileKey] = record
	}
	if record.ScopeKey == "" {
		record.ScopeKey = scopeKey
	}
	if record.SizeBytes <= 0 {
		record.SizeBytes = int64(len(content))
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.LastUsedAt = now
	scope := state.Scopes[scopeKey]
	if scope != nil {
		scope.LastUsedAt = now
	}

	if record.Token == "" {
		if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, resolvedPath, content, contentSHA); err != nil {
			if isPreviewParentMissingError(err) {
				clearPreviewScope(state, scopeKey)
				scopeFolder, err = p.ensureScopeFolderLocked(ctx, state, scopeKey, principals)
				if err != nil {
					return "", true, err
				}
				record.Token = ""
				record.URL = ""
				record.Shared = nil
				if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, resolvedPath, content, contentSHA); err != nil {
					return "", true, err
				}
			} else {
				return "", true, err
			}
		}
	}

	if record.URL == "" {
		url, err := p.api.QueryMetaURL(ctx, record.Token, previewFileType)
		if err != nil {
			return "", true, fmt.Errorf("query markdown preview url for %s: %w", resolvedPath, err)
		}
		record.URL = url
	}

	if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, &record.Shared, principals); err != nil {
		if isPreviewResourceMissingError(err) {
			record.Token = ""
			record.URL = ""
			record.Shared = nil
			if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, resolvedPath, content, contentSHA); err != nil {
				return "", true, err
			}
			if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, &record.Shared, principals); err != nil {
				return "", true, err
			}
		} else {
			return "", true, err
		}
	}

	return record.URL, true, nil
}

func (p *DriveMarkdownPreviewer) uploadPreviewFileLocked(ctx context.Context, record *previewFileRecord, parentToken, resolvedPath string, content []byte, contentSHA string) error {
	fileToken, err := p.api.UploadFile(ctx, parentToken, previewFileName(resolvedPath, contentSHA), content)
	if err != nil {
		return fmt.Errorf("upload markdown preview for %s: %w", resolvedPath, err)
	}
	record.Token = fileToken
	record.URL = ""
	record.Shared = map[string]bool{}
	return nil
}

func (p *DriveMarkdownPreviewer) ensureScopeFolderLocked(ctx context.Context, state *previewState, scopeKey string, principals []previewPrincipal) (*previewFolderRecord, error) {
	scope := state.Scopes[scopeKey]
	if scope == nil {
		scope = &previewScopeRecord{}
		state.Scopes[scopeKey] = scope
	}

	for attempt := 0; attempt < 2; attempt++ {
		root, err := p.ensureRootFolderLocked(ctx, state)
		if err != nil {
			return nil, err
		}

		if scope.Folder == nil {
			scope.Folder = &previewFolderRecord{}
		}
		if scope.Folder.Token == "" {
			node, err := p.api.CreateFolder(ctx, previewScopeFolderName(scopeKey), root.Token)
			if err != nil {
				if isPreviewParentMissingError(err) {
					state.Root = nil
					continue
				}
				return nil, fmt.Errorf("create markdown preview folder for %s: %w", scopeKey, err)
			}
			scope.Folder.Token = node.Token
			scope.Folder.URL = node.URL
			scope.Folder.Shared = map[string]bool{}
		}

		if err := ensurePreviewPermissions(ctx, p.api, scope.Folder.Token, previewFolderType, &scope.Folder.Shared, principals); err != nil {
			if isPreviewResourceMissingError(err) {
				scope.Folder = nil
				continue
			}
			return nil, fmt.Errorf("authorize markdown preview folder for %s: %w", scopeKey, err)
		}
		return scope.Folder, nil
	}

	return nil, fmt.Errorf("create markdown preview folder for %s: exhausted retries", scopeKey)
}

func (p *DriveMarkdownPreviewer) ensureRootFolderLocked(ctx context.Context, state *previewState) (*previewFolderRecord, error) {
	if state.Root == nil {
		state.Root = &previewFolderRecord{}
	}
	if state.Root.Token != "" {
		return state.Root, nil
	}

	node, err := p.api.CreateFolder(ctx, p.config.RootFolderName, "")
	if err != nil {
		return nil, fmt.Errorf("create markdown preview root folder: %w", err)
	}
	state.Root.Token = node.Token
	state.Root.URL = node.URL
	return state.Root, nil
}

func (p *DriveMarkdownPreviewer) resolveMarkdownPath(rawTarget string, req MarkdownPreviewRequest) (string, bool, error) {
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return "", false, nil
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimPrefix(strings.TrimSuffix(target, ">"), "<")
	}
	if idx := strings.IndexAny(target, " \t\n"); idx >= 0 {
		target = target[:idx]
	}
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
		return "", false, nil
	}

	cleanTarget, _ := stripMarkdownLocationSuffix(target)
	if !strings.EqualFold(filepath.Ext(cleanTarget), ".md") {
		return "", false, nil
	}

	roots := previewAllowedRoots(req.ThreadCWD, req.WorkspaceRoot, p.config.ProcessCWD)
	candidates := previewPathCandidates(cleanTarget, roots)
	for _, candidate := range candidates {
		resolved, err := previewCanonicalPath(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", true, err
		}
		if !previewPathWithinAnyRoot(resolved, roots) {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", true, err
		}
		if info.IsDir() {
			continue
		}
		return resolved, true, nil
	}
	return "", false, nil
}
