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

func (p *DriveMarkdownPreviewer) RewriteFinalBlock(ctx context.Context, req FinalBlockPreviewRequest) (FinalBlockPreviewResult, error) {
	result := FinalBlockPreviewResult{Block: req.Block}
	if p == nil {
		return result, nil
	}
	if !result.Block.Final || result.Block.Kind != render.BlockAssistantMarkdown || strings.TrimSpace(result.Block.Text) == "" {
		return result, nil
	}

	principals := previewPrincipals(req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	if len(principals) == 0 {
		return result, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadStateLocked()
	runtime := &previewRewriteRuntime{}
	rewritten, supplements, changed, dirty, rewriteErr := p.rewriteMarkdownLinksLocked(ctx, state, req, principals, runtime)
	if changed {
		result.Block.Text = rewritten
	}
	result.Supplements = append(result.Supplements, supplements...)
	if changed || dirty {
		if err := p.saveStateLocked(); err != nil && rewriteErr == nil {
			rewriteErr = err
		}
	}
	return result, rewriteErr
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinksLocked(ctx context.Context, state *previewState, req FinalBlockPreviewRequest, principals []previewPrincipal, runtime *previewRewriteRuntime) (string, []PreviewSupplement, bool, bool, error) {
	text := req.Block.Text
	matches := markdownLinkPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil, false, false, nil
	}

	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	rewrittenTargets := map[string]string{}
	var errs []string
	var supplements []PreviewSupplement

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
			ref := PreviewReference{
				RawTarget:   rawTarget,
				TargetStart: targetStart,
				TargetEnd:   targetEnd,
			}
			published, ok, err := p.materializePreviewTargetLocked(ctx, state, ref, req, scopeKey, principals, runtime)
			switch {
			case err != nil:
				errs = append(errs, err.Error())
			case ok && published != nil:
				if published.Mode == PreviewPublishModeInlineLink && strings.TrimSpace(published.URL) != "" {
					replacement = published.URL
					rewrittenTargets[rawTarget] = published.URL
					changed = true
				} else {
					rewrittenTargets[rawTarget] = rawTarget
				}
				if len(published.Supplements) > 0 {
					supplements = append(supplements, published.Supplements...)
				}
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
	return builder.String(), supplements, changed, runtime != nil && runtime.dirty, rewriteErr
}

func (p *DriveMarkdownPreviewer) materializePreviewTargetLocked(ctx context.Context, state *previewState, ref PreviewReference, req FinalBlockPreviewRequest, scopeKey string, principals []previewPrincipal, runtime *previewRewriteRuntime) (*PreviewPublishResult, bool, error) {
	var errs []string
	for _, handler := range p.handlers {
		if handler == nil || !handler.Match(req, ref) {
			continue
		}
		plan, ok, err := handler.Plan(ctx, req, ref)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if !ok || plan == nil {
			continue
		}
		result, published, publishErr := p.publishPreviewPlanLocked(ctx, state, req, *plan, scopeKey, principals, runtime)
		if publishErr != nil {
			errs = append(errs, publishErr.Error())
			continue
		}
		if published {
			return result, true, nil
		}
	}
	if len(errs) > 0 {
		return nil, true, errors.New(strings.Join(errs, "; "))
	}
	return nil, false, nil
}

func (p *DriveMarkdownPreviewer) publishPreviewPlanLocked(ctx context.Context, state *previewState, req FinalBlockPreviewRequest, plan PreviewPlan, scopeKey string, principals []previewPrincipal, runtime *previewRewriteRuntime) (*PreviewPublishResult, bool, error) {
	var errs []string
	for _, delivery := range plan.Deliveries {
		for _, publisher := range p.publishers {
			if publisher == nil || !publisher.Supports(delivery, plan.Artifact) {
				continue
			}
			result, ok, err := publisher.Publish(ctx, PreviewPublishRequest{
				Request:    req,
				Plan:       plan,
				Delivery:   delivery,
				State:      state,
				ScopeKey:   scopeKey,
				Principals: principals,
				Runtime:    runtime,
			})
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if ok && result != nil {
				return result, true, nil
			}
		}
	}
	if len(errs) > 0 {
		return nil, false, errors.New(strings.Join(errs, "; "))
	}
	return nil, false, nil
}

type markdownFilePreviewHandler struct {
	previewer *DriveMarkdownPreviewer
}

func (h markdownFilePreviewHandler) ID() string { return "markdown_file" }

func (h markdownFilePreviewHandler) Match(_ FinalBlockPreviewRequest, ref PreviewReference) bool {
	target := strings.TrimSpace(ref.RawTarget)
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimPrefix(strings.TrimSuffix(target, ">"), "<")
	}
	if idx := strings.IndexAny(target, " \t\n"); idx >= 0 {
		target = target[:idx]
	}
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
		return false
	}
	cleanTarget, _ := stripMarkdownLocationSuffix(target)
	_, _, ok := previewArtifactMetadata(cleanTarget)
	return ok
}

func (h markdownFilePreviewHandler) Plan(_ context.Context, req FinalBlockPreviewRequest, ref PreviewReference) (*PreviewPlan, bool, error) {
	if h.previewer == nil {
		return nil, false, nil
	}
	resolvedPath, ok, err := h.previewer.resolvePreviewPath(ref.RawTarget, req)
	if err != nil || !ok {
		return nil, ok, err
	}
	artifactKind, mimeType, supported := previewArtifactMetadata(resolvedPath)
	if !supported {
		return nil, false, nil
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, true, fmt.Errorf("read preview source %s: %w", resolvedPath, err)
	}
	if len(content) == 0 {
		return nil, true, fmt.Errorf("skip empty preview source %s", resolvedPath)
	}
	if int64(len(content)) > h.previewer.config.MaxFileBytes {
		return nil, true, fmt.Errorf("preview source exceeds %d bytes: %s", h.previewer.config.MaxFileBytes, resolvedPath)
	}

	sum := sha256.Sum256(content)
	contentSHA := hex.EncodeToString(sum[:])
	return &PreviewPlan{
		HandlerID: h.ID(),
		Artifact: PreparedPreviewArtifact{
			SourcePath:   resolvedPath,
			DisplayName:  filepath.Base(resolvedPath),
			ContentHash:  contentSHA,
			ArtifactKind: artifactKind,
			MIMEType:     mimeType,
			Text:         string(content),
			Bytes:        append([]byte(nil), content...),
		},
		Deliveries: []PreviewDeliveryPlan{{
			Kind: PreviewDeliveryDriveFileLink,
		}},
	}, true, nil
}

type driveMarkdownLinkPublisher struct {
	previewer *DriveMarkdownPreviewer
}

func (p driveMarkdownLinkPublisher) ID() string { return "drive_markdown_link" }

func (p driveMarkdownLinkPublisher) Supports(delivery PreviewDeliveryPlan, artifact PreparedPreviewArtifact) bool {
	return p.previewer != nil &&
		p.previewer.api != nil &&
		delivery.Kind == PreviewDeliveryDriveFileLink &&
		isSupportedPreviewArtifactKind(artifact.ArtifactKind)
}

func (p driveMarkdownLinkPublisher) Publish(ctx context.Context, req PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	if p.previewer == nil {
		return nil, false, nil
	}
	return p.previewer.publishDriveMarkdownLinkLocked(ctx, req)
}

func (p *DriveMarkdownPreviewer) publishDriveMarkdownLinkLocked(ctx context.Context, req PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	artifact := req.Plan.Artifact
	if strings.TrimSpace(artifact.SourcePath) == "" || len(artifact.Bytes) == 0 {
		return nil, false, nil
	}

	scopeFolder, err := p.ensureScopeFolderLocked(ctx, req.State, req.ScopeKey, req.Principals)
	if err != nil {
		return nil, true, err
	}

	fileKey := previewFileKey(req.ScopeKey, artifact.SourcePath, artifact.ContentHash)
	record := req.State.Files[fileKey]
	if record == nil {
		record = &previewFileRecord{
			Path:      artifact.SourcePath,
			SHA256:    artifact.ContentHash,
			ScopeKey:  req.ScopeKey,
			SizeBytes: int64(len(artifact.Bytes)),
		}
		req.State.Files[fileKey] = record
	}
	if record.ScopeKey == "" {
		record.ScopeKey = req.ScopeKey
	}
	if record.SizeBytes <= 0 {
		record.SizeBytes = int64(len(artifact.Bytes))
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.LastUsedAt = now
	scope := req.State.Scopes[req.ScopeKey]
	if scope != nil {
		scope.LastUsedAt = now
	}

	if record.Token == "" {
		if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, artifact.SourcePath, artifact.Bytes, artifact.ContentHash); err != nil {
			if isPreviewParentMissingError(err) {
				clearPreviewScope(req.State, req.ScopeKey)
				scopeFolder, err = p.ensureScopeFolderLocked(ctx, req.State, req.ScopeKey, req.Principals)
				if err != nil {
					return nil, true, err
				}
				record.Token = ""
				record.URL = ""
				record.Shared = nil
				if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, artifact.SourcePath, artifact.Bytes, artifact.ContentHash); err != nil {
					return nil, true, err
				}
			} else {
				return nil, true, err
			}
		}
	}

	if record.URL == "" {
		url, err := p.api.QueryMetaURL(ctx, record.Token, previewFileType)
		if err != nil {
			return nil, true, fmt.Errorf("query preview url for %s: %w", artifact.SourcePath, err)
		}
		record.URL = url
	}

	if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, &record.Shared, req.Principals); err != nil {
		if isPreviewResourceMissingError(err) {
			record.Token = ""
			record.URL = ""
			record.Shared = nil
			if err := p.uploadPreviewFileLocked(ctx, record, scopeFolder.Token, artifact.SourcePath, artifact.Bytes, artifact.ContentHash); err != nil {
				return nil, true, err
			}
			if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, &record.Shared, req.Principals); err != nil {
				return nil, true, err
			}
		} else {
			return nil, true, err
		}
	}

	return &PreviewPublishResult{
		PublisherID: p.driveMarkdownPublisherID(),
		Mode:        PreviewPublishModeInlineLink,
		URL:         record.URL,
	}, true, nil
}

func (p *DriveMarkdownPreviewer) driveMarkdownPublisherID() string {
	return driveMarkdownLinkPublisher{previewer: p}.ID()
}

func (p *DriveMarkdownPreviewer) uploadPreviewFileLocked(ctx context.Context, record *previewFileRecord, parentToken, resolvedPath string, content []byte, contentSHA string) error {
	fileToken, err := p.api.UploadFile(ctx, parentToken, previewFileName(resolvedPath, contentSHA), content)
	if err != nil {
		return fmt.Errorf("upload preview for %s: %w", resolvedPath, err)
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
				return nil, fmt.Errorf("create preview folder for %s: %w", scopeKey, err)
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
			return nil, fmt.Errorf("authorize preview folder for %s: %w", scopeKey, err)
		}
		return scope.Folder, nil
	}

	return nil, fmt.Errorf("create preview folder for %s: exhausted retries", scopeKey)
}

func (p *DriveMarkdownPreviewer) ensureRootFolderLocked(ctx context.Context, state *previewState) (*previewFolderRecord, error) {
	if state.Root == nil {
		state.Root = &previewFolderRecord{}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if state.Root.Token == "" {
			node, ok, err := p.discoverManagedRootLocked(ctx)
			if err != nil {
				return nil, fmt.Errorf("discover preview root folder: %w", err)
			}
			if ok {
				state.Root.Token = node.Token
				state.Root.URL = node.URL
			}
		}

		if state.Root.Token == "" {
			node, err := p.api.CreateFolder(ctx, defaultPreviewRootFolderName, "")
			if err != nil {
				return nil, fmt.Errorf("create preview root folder: %w", err)
			}
			state.Root.Token = node.Token
			state.Root.URL = node.URL
		}
		return state.Root, nil
	}

	return nil, fmt.Errorf("create preview root folder: exhausted retries")
}

func (p *DriveMarkdownPreviewer) resolvePreviewPath(rawTarget string, req MarkdownPreviewRequest) (string, bool, error) {
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
	if _, _, ok := previewArtifactMetadata(cleanTarget); !ok {
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
