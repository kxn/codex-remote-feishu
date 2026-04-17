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
	"unicode"
	"unicode/utf8"

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

	runtime := &previewRewriteRuntime{}
	rewritten, supplements, changed, dirty, rewriteErr := p.rewriteMarkdownLinks(ctx, req, principals, runtime)
	if changed {
		result.Block.Text = rewritten
	}
	result.Supplements = append(result.Supplements, supplements...)
	if changed || dirty {
		p.stateMu.Lock()
		if err := p.saveStateLocked(); err != nil && rewriteErr == nil {
			rewriteErr = err
		}
		p.stateMu.Unlock()
	}
	return result, rewriteErr
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinks(ctx context.Context, req FinalBlockPreviewRequest, principals []previewPrincipal, runtime *previewRewriteRuntime) (string, []PreviewSupplement, bool, bool, error) {
	text := req.Block.Text
	segments := splitFinalCardFenceSegments(text)
	if len(segments) == 0 {
		return text, nil, false, false, nil
	}

	scopeKey := previewScopeKey(req.GatewayID, req.SurfaceSessionID, req.ChatID, req.ActorUserID)
	rewrittenTargets := map[string]string{}
	var errs []string
	var supplements []PreviewSupplement

	var builder strings.Builder
	changed := false
	offset := 0
	for _, segment := range segments {
		if segment.fenced {
			builder.WriteString(segment.text)
			offset += len(segment.text)
			continue
		}
		rewrittenSegment, segmentSupplements, segmentChanged, segmentErrs := p.rewriteMarkdownLinksInline(
			ctx,
			req,
			principals,
			runtime,
			scopeKey,
			rewrittenTargets,
			segment.text,
			offset,
		)
		builder.WriteString(rewrittenSegment)
		if segmentChanged {
			changed = true
		}
		if len(segmentSupplements) > 0 {
			supplements = append(supplements, segmentSupplements...)
		}
		if len(segmentErrs) > 0 {
			errs = append(errs, segmentErrs...)
		}
		offset += len(segment.text)
	}

	var rewriteErr error
	if len(errs) > 0 {
		rewriteErr = errors.New(strings.Join(errs, "; "))
	}
	return builder.String(), supplements, changed, runtime != nil && runtime.dirty, rewriteErr
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinksInline(
	ctx context.Context,
	req FinalBlockPreviewRequest,
	principals []previewPrincipal,
	runtime *previewRewriteRuntime,
	scopeKey string,
	rewrittenTargets map[string]string,
	text string,
	baseOffset int,
) (string, []PreviewSupplement, bool, []string) {
	if text == "" {
		return "", nil, false, nil
	}

	var (
		builder     strings.Builder
		supplements []PreviewSupplement
		errs        []string
		changed     bool
	)
	last := 0
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		run := consecutiveByteRun(text, i, '`')
		close := closingBacktickRun(text, i+run, run)
		if close < 0 {
			break
		}
		rewritten, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewriteMarkdownLinksPlain(
			ctx,
			req,
			principals,
			runtime,
			scopeKey,
			rewrittenTargets,
			text[last:i],
			baseOffset+last,
		)
		builder.WriteString(rewritten)
		if rewrittenChanged {
			changed = true
		}
		if len(rewrittenSupplements) > 0 {
			supplements = append(supplements, rewrittenSupplements...)
		}
		if len(rewrittenErrs) > 0 {
			errs = append(errs, rewrittenErrs...)
		}
		inlineRewritten, inlineSupplements, inlineChanged, inlineErrs := p.rewriteMarkdownLinksCodeSpan(
			ctx,
			req,
			principals,
			runtime,
			scopeKey,
			rewrittenTargets,
			text[i:close+run],
			text[i+run:close],
			baseOffset+i+run,
		)
		builder.WriteString(inlineRewritten)
		if inlineChanged {
			changed = true
		}
		if len(inlineSupplements) > 0 {
			supplements = append(supplements, inlineSupplements...)
		}
		if len(inlineErrs) > 0 {
			errs = append(errs, inlineErrs...)
		}
		i = close + run
		last = i
	}

	rewritten, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewriteMarkdownLinksPlain(
		ctx,
		req,
		principals,
		runtime,
		scopeKey,
		rewrittenTargets,
		text[last:],
		baseOffset+last,
	)
	builder.WriteString(rewritten)
	if rewrittenChanged {
		changed = true
	}
	if len(rewrittenSupplements) > 0 {
		supplements = append(supplements, rewrittenSupplements...)
	}
	if len(rewrittenErrs) > 0 {
		errs = append(errs, rewrittenErrs...)
	}
	return builder.String(), supplements, changed, errs
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinksCodeSpan(
	ctx context.Context,
	req FinalBlockPreviewRequest,
	principals []previewPrincipal,
	runtime *previewRewriteRuntime,
	scopeKey string,
	rewrittenTargets map[string]string,
	rawSpan string,
	text string,
	baseOffset int,
) (string, []PreviewSupplement, bool, []string) {
	if text == "" {
		return rawSpan, nil, false, nil
	}
	trimStart, trimEnd := trimMarkdownInlineSpaceBounds(text)
	if trimStart >= trimEnd {
		return rawSpan, nil, false, nil
	}
	trimmed := text[trimStart:trimEnd]
	if end, label, rawTarget, ok := parseMarkdownLinkAt(trimmed, 0); ok && end == len(trimmed) {
		replacement, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewritePreviewReferenceTarget(
			ctx,
			req,
			principals,
			runtime,
			scopeKey,
			rewrittenTargets,
			rawTarget,
			baseOffset+trimStart+len(label)+3,
			baseOffset+trimStart+len(label)+3+len(rawTarget),
		)
		if !rewrittenChanged {
			return rawSpan, rewrittenSupplements, false, rewrittenErrs
		}
		return "[" + label + "](" + replacement + ")", rewrittenSupplements, true, rewrittenErrs
	}
	rawTarget, display, ok := parseStandalonePreviewReferenceWhole(trimmed)
	if !ok {
		return rawSpan, nil, false, nil
	}
	replacement, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewritePreviewReferenceTarget(
		ctx,
		req,
		principals,
		runtime,
		scopeKey,
		rewrittenTargets,
		rawTarget,
		baseOffset+trimStart,
		baseOffset+trimStart+len(rawTarget),
	)
	if !rewrittenChanged {
		return rawSpan, rewrittenSupplements, false, rewrittenErrs
	}
	return "[" + display + "](" + replacement + ")", rewrittenSupplements, true, rewrittenErrs
}

func (p *DriveMarkdownPreviewer) rewriteMarkdownLinksPlain(
	ctx context.Context,
	req FinalBlockPreviewRequest,
	principals []previewPrincipal,
	runtime *previewRewriteRuntime,
	scopeKey string,
	rewrittenTargets map[string]string,
	text string,
	baseOffset int,
) (string, []PreviewSupplement, bool, []string) {
	if text == "" {
		return "", nil, false, nil
	}

	var (
		builder     strings.Builder
		supplements []PreviewSupplement
		errs        []string
		changed     bool
	)
	last := 0
	for i := 0; i < len(text); {
		if text[i] == '[' {
			end, label, rawTarget, ok := parseMarkdownLinkAt(text, i)
			if ok {
				builder.WriteString(text[last:i])
				replacement, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewritePreviewReferenceTarget(
					ctx,
					req,
					principals,
					runtime,
					scopeKey,
					rewrittenTargets,
					rawTarget,
					baseOffset+i+len(label)+3,
					baseOffset+i+len(label)+3+len(rawTarget),
				)
				if rewrittenChanged {
					builder.WriteByte('[')
					builder.WriteString(label)
					builder.WriteString("](")
					builder.WriteString(replacement)
					builder.WriteByte(')')
					changed = true
				} else {
					builder.WriteString(text[i:end])
				}
				if len(rewrittenSupplements) > 0 {
					supplements = append(supplements, rewrittenSupplements...)
				}
				if len(rewrittenErrs) > 0 {
					errs = append(errs, rewrittenErrs...)
				}
				i = end
				last = i
				continue
			}
		}
		end, rawTarget, display, ok := parseStandalonePreviewReferenceAt(text, i)
		if !ok {
			i++
			continue
		}
		builder.WriteString(text[last:i])
		replacement, rewrittenSupplements, rewrittenChanged, rewrittenErrs := p.rewritePreviewReferenceTarget(
			ctx,
			req,
			principals,
			runtime,
			scopeKey,
			rewrittenTargets,
			rawTarget,
			baseOffset+i,
			baseOffset+i+len(rawTarget),
		)
		if rewrittenChanged {
			builder.WriteByte('[')
			builder.WriteString(display)
			builder.WriteString("](")
			builder.WriteString(replacement)
			builder.WriteByte(')')
			changed = true
		} else {
			builder.WriteString(text[i:end])
		}
		if len(rewrittenSupplements) > 0 {
			supplements = append(supplements, rewrittenSupplements...)
		}
		if len(rewrittenErrs) > 0 {
			errs = append(errs, rewrittenErrs...)
		}
		i = end
		last = i
	}
	builder.WriteString(text[last:])
	return builder.String(), supplements, changed, errs
}

func (p *DriveMarkdownPreviewer) rewritePreviewReferenceTarget(
	ctx context.Context,
	req FinalBlockPreviewRequest,
	principals []previewPrincipal,
	runtime *previewRewriteRuntime,
	scopeKey string,
	rewrittenTargets map[string]string,
	rawTarget string,
	targetStart int,
	targetEnd int,
) (string, []PreviewSupplement, bool, []string) {
	replacement := rawTarget
	if cached, ok := rewrittenTargets[rawTarget]; ok {
		return cached, nil, cached != rawTarget, nil
	}
	ref := PreviewReference{
		RawTarget:   rawTarget,
		TargetStart: targetStart,
		TargetEnd:   targetEnd,
		Location:    previewReferenceLocation(rawTarget),
	}
	published, publishedOK, err := p.materializePreviewTarget(ctx, ref, req, scopeKey, principals, runtime)
	if err != nil {
		rewrittenTargets[rawTarget] = replacement
		return replacement, nil, false, []string{err.Error()}
	}
	var supplements []PreviewSupplement
	if publishedOK && published != nil {
		if published.Mode == PreviewPublishModeInlineLink && strings.TrimSpace(published.URL) != "" {
			replacement = published.URL
		}
		if len(published.Supplements) > 0 {
			supplements = append(supplements, published.Supplements...)
		}
	}
	rewrittenTargets[rawTarget] = replacement
	return replacement, supplements, replacement != rawTarget, nil
}

func previewReferenceLocation(rawTarget string) PreviewLocation {
	_, location, _ := splitPreviewLocationSuffix(rawTarget)
	return location
}

func trimMarkdownInlineSpaceBounds(text string) (int, int) {
	start := 0
	for start < len(text) {
		r, size := utf8.DecodeRuneInString(text[start:])
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}
	end := len(text)
	for end > start {
		r, size := utf8.DecodeLastRuneInString(text[:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	return start, end
}

func parseStandalonePreviewReferenceWhole(text string) (rawTarget, display string, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	if !looksLikeStandalonePreviewTarget(text) {
		return "", "", false
	}
	return normalizeStandalonePreviewDisplay(text), normalizeStandalonePreviewDisplay(text), true
}

func parseStandalonePreviewReferenceAt(text string, start int) (end int, rawTarget, display string, ok bool) {
	if start < 0 || start >= len(text) || !hasStandalonePreviewBoundaryBefore(text, start) {
		return 0, "", "", false
	}
	if text[start] == '<' {
		closeOffset := strings.IndexByte(text[start+1:], '>')
		if closeOffset < 0 {
			return 0, "", "", false
		}
		candidateEnd := start + 1 + closeOffset + 1
		if !hasStandalonePreviewBoundaryAfter(text, candidateEnd) {
			return 0, "", "", false
		}
		candidate := text[start:candidateEnd]
		if !looksLikeStandalonePreviewTarget(candidate) {
			return 0, "", "", false
		}
		display = normalizeStandalonePreviewDisplay(candidate)
		return candidateEnd, display, display, true
	}
	scanEnd := start
	for scanEnd < len(text) && text[scanEnd] != '`' {
		r, size := utf8.DecodeRuneInString(text[scanEnd:])
		if unicode.IsSpace(r) || isStandalonePreviewTerminator(r) {
			break
		}
		scanEnd += size
	}
	if scanEnd <= start {
		return 0, "", "", false
	}
	trimmedEnd := trimStandalonePreviewTrailingPunctuation(text[start:scanEnd])
	if trimmedEnd <= 0 {
		return 0, "", "", false
	}
	candidateEnd := start + trimmedEnd
	if !hasStandalonePreviewBoundaryAfter(text, candidateEnd) {
		return 0, "", "", false
	}
	candidate := text[start:candidateEnd]
	if !looksLikeStandalonePreviewTarget(candidate) {
		return 0, "", "", false
	}
	display = normalizeStandalonePreviewDisplay(candidate)
	return candidateEnd, display, display, true
}

func isStandalonePreviewTerminator(r rune) bool {
	return strings.ContainsRune(",;!?，。；！？、)]}\"'》）】>”’", r)
}

func hasStandalonePreviewBoundaryBefore(text string, start int) bool {
	if start <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return unicode.IsSpace(r) || strings.ContainsRune("([{\"'<,.;:!?，。；：！？、（【《“‘", r)
}

func hasStandalonePreviewBoundaryAfter(text string, end int) bool {
	if end >= len(text) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(text[end:])
	return unicode.IsSpace(r) || strings.ContainsRune(")]}\"'>,.;:!?，。；：！？、）】》”’", r)
}

func trimStandalonePreviewTrailingPunctuation(text string) int {
	end := len(text)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:end])
		switch r {
		case '.', ',', ';', '!', '?', '，', '。', '；', '！', '？', '"', '\'', '”', '’':
			end -= size
			continue
		case ')', '）':
			if strings.ContainsRune(text[:end-size], '(') || strings.ContainsRune(text[:end-size], '（') {
				return end
			}
			end -= size
			continue
		case ']', '】':
			if strings.ContainsRune(text[:end-size], '[') || strings.ContainsRune(text[:end-size], '【') {
				return end
			}
			end -= size
			continue
		case '}', '》':
			end -= size
			continue
		default:
			return end
		}
	}
	return end
}

func normalizeStandalonePreviewDisplay(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<") && strings.HasSuffix(text, ">") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "<"), ">"))
	}
	return text
}

func looksLikeStandalonePreviewTarget(text string) bool {
	target := normalizeStandalonePreviewDisplay(text)
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.ContainsAny(target, " \t\r\n") {
		return false
	}
	cleanTarget, _ := stripMarkdownLocationSuffix(target)
	if cleanTarget == "" {
		return false
	}
	if _, ok := previewLexicalAbsolutePath(cleanTarget); ok {
		return true
	}
	if strings.HasPrefix(cleanTarget, "./") || strings.HasPrefix(cleanTarget, "../") {
		return true
	}
	if strings.ContainsAny(cleanTarget, `/\`) {
		return true
	}
	base := filepath.Base(cleanTarget)
	if strings.HasPrefix(base, ".") {
		return true
	}
	return filepath.Ext(base) != ""
}

func (p *DriveMarkdownPreviewer) materializePreviewTarget(ctx context.Context, ref PreviewReference, req FinalBlockPreviewRequest, scopeKey string, principals []previewPrincipal, runtime *previewRewriteRuntime) (*PreviewPublishResult, bool, error) {
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
		result, published, publishErr := p.publishPreviewPlan(ctx, req, ref, *plan, scopeKey, principals, runtime)
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

func (p *DriveMarkdownPreviewer) publishPreviewPlan(ctx context.Context, req FinalBlockPreviewRequest, ref PreviewReference, plan PreviewPlan, scopeKey string, principals []previewPrincipal, runtime *previewRewriteRuntime) (*PreviewPublishResult, bool, error) {
	var errs []string
	for _, delivery := range plan.Deliveries {
		for _, publisher := range p.publishers {
			if publisher == nil || !publisher.Supports(delivery, plan.Artifact) {
				continue
			}
			result, ok, err := publisher.Publish(ctx, PreviewPublishRequest{
				Request:    req,
				Reference:  ref,
				Plan:       plan,
				Delivery:   delivery,
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
	cleanTarget, _, _ := splitPreviewLocationSuffix(target)
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
	rendererKind := previewRendererKind(resolvedPath, artifactKind, mimeType)
	deliveries := []PreviewDeliveryPlan{{
		Kind: PreviewDeliveryWebFileLink,
	}}
	if isSupportedPreviewArtifactKind(artifactKind) {
		deliveries = append([]PreviewDeliveryPlan{{
			Kind: PreviewDeliveryDriveFileLink,
		}}, deliveries...)
	}
	return &PreviewPlan{
		HandlerID: h.ID(),
		Artifact: PreparedPreviewArtifact{
			SourcePath:   resolvedPath,
			DisplayName:  filepath.Base(resolvedPath),
			ContentHash:  contentSHA,
			ArtifactKind: artifactKind,
			MIMEType:     mimeType,
			RendererKind: rendererKind,
			Text:         string(content),
			Bytes:        append([]byte(nil), content...),
		},
		Deliveries: deliveries,
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
	return p.previewer.publishDriveMarkdownLink(ctx, req)
}

func (p *DriveMarkdownPreviewer) publishDriveMarkdownLink(ctx context.Context, req PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	artifact := req.Plan.Artifact
	if strings.TrimSpace(artifact.SourcePath) == "" || len(artifact.Bytes) == 0 {
		return nil, false, nil
	}

	fileKey := previewFileKey(req.ScopeKey, artifact.SourcePath, artifact.ContentHash)
	now := p.nowUTC()
	if record, ok := p.markDriveFileUsedIfReady(fileKey, req.ScopeKey, int64(len(artifact.Bytes)), req.Principals, now, req.Runtime); ok {
		return &PreviewPublishResult{
			PublisherID: p.driveMarkdownPublisherID(),
			Mode:        PreviewPublishModeInlineLink,
			URL:         record.URL,
		}, true, nil
	}

	value, err := p.doPreviewOp("drive-file:"+fileKey, func() (any, error) {
		if record, ok := p.markDriveFileUsedIfReady(fileKey, req.ScopeKey, int64(len(artifact.Bytes)), req.Principals, now, req.Runtime); ok {
			return record, nil
		}
		return p.publishDriveFile(ctx, req, fileKey, now)
	})
	if err != nil {
		return nil, true, err
	}
	record, _ := value.(*previewFileRecord)
	if record == nil || strings.TrimSpace(record.URL) == "" {
		return nil, false, nil
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

func (p *DriveMarkdownPreviewer) publishDriveFile(ctx context.Context, req PreviewPublishRequest, fileKey string, now time.Time) (*previewFileRecord, error) {
	artifact := req.Plan.Artifact
	for attempt := 0; attempt < 2; attempt++ {
		scopeFolder, err := p.ensureScopeFolder(ctx, req.ScopeKey, req.Principals, req.Runtime)
		if err != nil {
			return nil, err
		}

		record := p.snapshotDriveFileRecord(fileKey, req.ScopeKey, artifact, now)
		if record == nil {
			record = &previewFileRecord{}
		}

		if strings.TrimSpace(record.Token) == "" {
			if err := p.uploadPreviewFile(ctx, record, scopeFolder.Token, artifact.SourcePath, artifact.Bytes, artifact.ContentHash); err != nil {
				if isPreviewParentMissingError(err) {
					p.clearPreviewScopeState(req.ScopeKey, req.Runtime)
					continue
				}
				return nil, err
			}
		}

		if strings.TrimSpace(record.URL) == "" {
			url, err := p.api.QueryMetaURL(ctx, record.Token, previewFileType)
			if err != nil {
				if isPreviewResourceMissingError(err) {
					p.resetDriveFileRecord(fileKey, req.Runtime)
					record.Token = ""
					record.URL = ""
					record.Shared = map[string]bool{}
					continue
				}
				return nil, fmt.Errorf("query preview url for %s: %w", artifact.SourcePath, err)
			}
			record.URL = url
		}

		shared := clonePreviewShared(record.Shared)
		if err := ensurePreviewPermissions(ctx, p.api, record.Token, previewFileType, &shared, req.Principals); err != nil {
			if isPreviewResourceMissingError(err) {
				p.resetDriveFileRecord(fileKey, req.Runtime)
				record.Token = ""
				record.URL = ""
				record.Shared = map[string]bool{}
				continue
			}
			return nil, err
		}
		record.Shared = shared
		p.commitDriveFileRecord(fileKey, req.ScopeKey, record, now, req.Runtime)
		return record, nil
	}
	return nil, fmt.Errorf("publish preview for %s: exhausted retries", artifact.SourcePath)
}

func (p *DriveMarkdownPreviewer) uploadPreviewFile(ctx context.Context, record *previewFileRecord, parentToken, resolvedPath string, content []byte, contentSHA string) error {
	fileToken, err := p.api.UploadFile(ctx, parentToken, previewFileName(resolvedPath, contentSHA), content)
	if err != nil {
		return fmt.Errorf("upload preview for %s: %w", resolvedPath, err)
	}
	record.Token = fileToken
	record.URL = ""
	record.Shared = map[string]bool{}
	return nil
}

func (p *DriveMarkdownPreviewer) ensureScopeFolder(ctx context.Context, scopeKey string, principals []previewPrincipal, runtime *previewRewriteRuntime) (*previewFolderRecord, error) {
	if folder := p.snapshotPreviewScopeFolder(scopeKey); folder != nil &&
		strings.TrimSpace(folder.Token) != "" &&
		previewSharedCoversPrincipals(folder.Shared, principals) {
		return folder, nil
	}

	value, err := p.doPreviewOp("drive-scope:"+strings.TrimSpace(scopeKey), func() (any, error) {
		for attempt := 0; attempt < 2; attempt++ {
			folder := p.snapshotPreviewScopeFolder(scopeKey)
			if folder != nil &&
				strings.TrimSpace(folder.Token) != "" &&
				previewSharedCoversPrincipals(folder.Shared, principals) {
				return folder, nil
			}

			root, err := p.ensureRootFolder(ctx, runtime)
			if err != nil {
				return nil, err
			}

			if folder == nil {
				folder = &previewFolderRecord{}
			}
			if strings.TrimSpace(folder.Token) == "" {
				node, err := p.api.CreateFolder(ctx, previewScopeFolderName(scopeKey), root.Token)
				if err != nil {
					if isPreviewParentMissingError(err) {
						p.clearPreviewRoot(runtime)
						continue
					}
					return nil, fmt.Errorf("create preview folder for %s: %w", scopeKey, err)
				}
				folder.Token = node.Token
				folder.URL = node.URL
				folder.Shared = map[string]bool{}
			}

			shared := clonePreviewShared(folder.Shared)
			if err := ensurePreviewPermissions(ctx, p.api, folder.Token, previewFolderType, &shared, principals); err != nil {
				if isPreviewResourceMissingError(err) {
					p.clearPreviewScopeState(scopeKey, runtime)
					continue
				}
				return nil, fmt.Errorf("authorize preview folder for %s: %w", scopeKey, err)
			}
			folder.Shared = shared
			p.storePreviewScopeFolder(scopeKey, folder, runtime)
			return folder, nil
		}
		return nil, fmt.Errorf("create preview folder for %s: exhausted retries", scopeKey)
	})
	if err != nil {
		return nil, err
	}
	folder, _ := value.(*previewFolderRecord)
	return folder, nil
}

func (p *DriveMarkdownPreviewer) ensureRootFolder(ctx context.Context, runtime *previewRewriteRuntime) (*previewFolderRecord, error) {
	if root := p.snapshotPreviewRoot(); root != nil && strings.TrimSpace(root.Token) != "" {
		return root, nil
	}

	value, err := p.doPreviewOp("drive-root", func() (any, error) {
		if root := p.snapshotPreviewRoot(); root != nil && strings.TrimSpace(root.Token) != "" {
			return root, nil
		}

		root := &previewFolderRecord{}
		node, ok, err := p.discoverManagedRoot(ctx)
		if err != nil {
			return nil, fmt.Errorf("discover preview root folder: %w", err)
		}
		if ok {
			root.Token = node.Token
			root.URL = node.URL
		}
		if strings.TrimSpace(root.Token) == "" {
			node, err := p.api.CreateFolder(ctx, defaultPreviewRootFolderName, "")
			if err != nil {
				return nil, fmt.Errorf("create preview root folder: %w", err)
			}
			root.Token = node.Token
			root.URL = node.URL
		}
		root.Shared = map[string]bool{}
		p.storePreviewRoot(root, runtime)
		return root, nil
	})
	if err != nil {
		return nil, err
	}
	root, _ := value.(*previewFolderRecord)
	return root, nil
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

	cleanTarget, _, _ := splitPreviewLocationSuffix(target)
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
