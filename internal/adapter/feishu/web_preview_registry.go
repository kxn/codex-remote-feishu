package feishu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultPreviewRecordTTL = 7 * 24 * time.Hour
	defaultPreviewBlobTTL   = 14 * 24 * time.Hour
)

var defaultPreviewCacheBudgetBytes int64 = 1 << 30

type webPreviewScopeManifest struct {
	ScopeKey      string                       `json:"scopeKey,omitempty"`
	ScopePublicID string                       `json:"scopePublicID,omitempty"`
	LastUsedAt    time.Time                    `json:"lastUsedAt,omitempty"`
	Records       map[string]*webPreviewRecord `json:"records,omitempty"`
}

type webPreviewRecord struct {
	PreviewID    string    `json:"previewID,omitempty"`
	LineageKey   string    `json:"lineageKey,omitempty"`
	PreviousID   string    `json:"previousPreviewID,omitempty"`
	SourcePath   string    `json:"sourcePath,omitempty"`
	DisplayName  string    `json:"displayName,omitempty"`
	ArtifactKind string    `json:"artifactKind,omitempty"`
	MIMEType     string    `json:"mimeType,omitempty"`
	RendererKind string    `json:"rendererKind,omitempty"`
	ContentHash  string    `json:"contentHash,omitempty"`
	BlobKey      string    `json:"blobKey,omitempty"`
	SizeBytes    int64     `json:"sizeBytes,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

type webPreviewArtifact struct {
	ScopePublicID string
	PreviewID     string
	Record        webPreviewRecord
	Content       []byte
}

type WebPreviewPublishResult struct {
	URL string
}

func previewScopePublicID(scopeKey string) string {
	return shortStablePreviewID("scope|" + strings.TrimSpace(scopeKey))
}

func previewLineageKey(scopeKey, sourcePath string) string {
	return strings.TrimSpace(scopeKey) + "|" + filepath.Clean(strings.TrimSpace(sourcePath))
}

func previewRecordReuseKey(lineageKey, contentHash string) string {
	return strings.TrimSpace(lineageKey) + "|" + strings.TrimSpace(contentHash)
}

func previewRecordID(scopeKey, sourcePath, contentHash string) string {
	return shortStablePreviewID("preview|" + previewRecordReuseKey(previewLineageKey(scopeKey, sourcePath), contentHash))
}

func shortStablePreviewID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:16]
}

func previewRendererKind(path, artifactKind, mimeType string) string {
	kind := strings.ToLower(strings.TrimSpace(artifactKind))
	switch kind {
	case "markdown":
		return "markdown"
	case "html":
		return "html_source"
	case "svg":
		return "svg_source"
	}
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "application/pdf":
		return "pdf"
	case "image/svg+xml":
		return "svg_source"
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	switch ext {
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html_source"
	case ".svg":
		return "svg_source"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".pdf":
		return "pdf"
	case ".txt", ".log", ".json", ".yaml", ".yml", ".xml", ".csv", ".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".sh", ".sql", ".ini", ".toml":
		return "text"
	default:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "text/") {
			return "text"
		}
	}
	return "download"
}

type webPreviewLinkPublisher struct {
	previewer *DriveMarkdownPreviewer
}

func (p webPreviewLinkPublisher) ID() string { return "web_preview_link" }

func (p webPreviewLinkPublisher) Supports(delivery PreviewDeliveryPlan, _ PreparedPreviewArtifact) bool {
	return p.previewer != nil && delivery.Kind == PreviewDeliveryWebFileLink
}

func (p webPreviewLinkPublisher) Publish(ctx context.Context, req PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	if p.previewer == nil {
		return nil, false, nil
	}
	return p.previewer.publishWebPreviewLinkLocked(ctx, req)
}

func (p *DriveMarkdownPreviewer) publishWebPreviewLinkLocked(ctx context.Context, req PreviewPublishRequest) (*PreviewPublishResult, bool, error) {
	if p == nil || p.webPublisher == nil {
		return nil, false, nil
	}
	result, err := p.publishWebPreviewArtifact(ctx, req)
	if err != nil {
		return nil, true, err
	}
	if strings.TrimSpace(result.URL) == "" {
		return nil, false, nil
	}
	return &PreviewPublishResult{
		PublisherID: webPreviewLinkPublisher{previewer: p}.ID(),
		Mode:        PreviewPublishModeInlineLink,
		URL:         result.URL,
	}, true, nil
}

func (p *DriveMarkdownPreviewer) publishWebPreviewArtifact(ctx context.Context, req PreviewPublishRequest) (WebPreviewPublishResult, error) {
	if p == nil || strings.TrimSpace(p.config.CacheDir) == "" {
		return WebPreviewPublishResult{}, fmt.Errorf("web preview cache dir is not configured")
	}
	artifact := req.Plan.Artifact
	if strings.TrimSpace(artifact.SourcePath) == "" || len(artifact.Bytes) == 0 {
		return WebPreviewPublishResult{}, fmt.Errorf("web preview artifact is empty")
	}
	scopeKey := strings.TrimSpace(req.ScopeKey)
	if strings.TrimSpace(scopeKey) == "" {
		return WebPreviewPublishResult{}, fmt.Errorf("web preview scope key is empty")
	}
	scopePublicID := previewScopePublicID(scopeKey)
	now := p.nowFn().UTC()

	p.webPreviewMu.Lock()
	manifest, err := p.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		p.webPreviewMu.Unlock()
		return WebPreviewPublishResult{}, err
	}
	if manifest == nil {
		manifest = &webPreviewScopeManifest{
			ScopeKey:      scopeKey,
			ScopePublicID: scopePublicID,
			Records:       map[string]*webPreviewRecord{},
		}
	}
	if manifest.ScopeKey == "" {
		manifest.ScopeKey = scopeKey
	}
	if manifest.ScopePublicID == "" {
		manifest.ScopePublicID = scopePublicID
	}
	lineageKey := previewLineageKey(scopeKey, artifact.SourcePath)
	previewID := previewRecordID(scopeKey, artifact.SourcePath, artifact.ContentHash)
	record := manifest.Records[previewID]
	if record == nil {
		record = &webPreviewRecord{
			PreviewID:    previewID,
			LineageKey:   lineageKey,
			SourcePath:   artifact.SourcePath,
			DisplayName:  firstNonEmpty(strings.TrimSpace(artifact.DisplayName), filepath.Base(artifact.SourcePath)),
			ArtifactKind: strings.TrimSpace(artifact.ArtifactKind),
			MIMEType:     strings.TrimSpace(artifact.MIMEType),
			RendererKind: firstNonEmpty(strings.TrimSpace(artifact.RendererKind), previewRendererKind(artifact.SourcePath, artifact.ArtifactKind, artifact.MIMEType)),
			ContentHash:  strings.TrimSpace(artifact.ContentHash),
			BlobKey:      strings.TrimSpace(artifact.ContentHash),
			SizeBytes:    int64(len(artifact.Bytes)),
			CreatedAt:    now,
			ExpiresAt:    now.Add(defaultPreviewRecordTTL),
		}
		if previous := findPreviousPreviewRecord(manifest, lineageKey, previewID); previous != nil {
			record.PreviousID = previous.PreviewID
		}
		manifest.Records[previewID] = record
	}
	record.LastUsedAt = now
	record.ExpiresAt = now.Add(defaultPreviewRecordTTL)
	if record.RendererKind == "" {
		record.RendererKind = previewRendererKind(artifact.SourcePath, artifact.ArtifactKind, artifact.MIMEType)
	}
	manifest.LastUsedAt = now
	if err := p.ensurePreviewBlob(record.BlobKey, artifact.Bytes); err != nil {
		p.webPreviewMu.Unlock()
		return WebPreviewPublishResult{}, err
	}
	if err := p.touchPreviewBlob(record.BlobKey, now); err != nil {
		p.webPreviewMu.Unlock()
		return WebPreviewPublishResult{}, err
	}
	if err := p.saveWebPreviewScopeManifest(manifest); err != nil {
		p.webPreviewMu.Unlock()
		return WebPreviewPublishResult{}, err
	}
	p.webPreviewMu.Unlock()

	url, err := p.issueWebPreviewURL(ctx, req, scopePublicID, previewID)
	if err != nil {
		return WebPreviewPublishResult{}, err
	}
	return WebPreviewPublishResult{URL: url}, nil
}

func findPreviousPreviewRecord(manifest *webPreviewScopeManifest, lineageKey, currentID string) *webPreviewRecord {
	if manifest == nil || len(manifest.Records) == 0 {
		return nil
	}
	candidates := make([]*webPreviewRecord, 0, len(manifest.Records))
	for _, record := range manifest.Records {
		if record == nil || record.LineageKey != lineageKey || record.PreviewID == currentID {
			continue
		}
		candidates = append(candidates, record)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}

func (p *DriveMarkdownPreviewer) issueWebPreviewURL(ctx context.Context, req PreviewPublishRequest, scopePublicID, previewID string) (string, error) {
	if p == nil || p.webPublisher == nil {
		return "", fmt.Errorf("web preview publisher is not configured")
	}
	prefix, err := p.webPublisher.IssueScopePrefix(ctx, webPreviewGrantRequest(req.Request, scopePublicID))
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(prefix))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("web preview prefix is empty")
	}
	parsed.Path = path.Join(strings.TrimRight(parsed.Path, "/"), previewID)
	if strings.HasSuffix(prefix, "/") {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	appendWebPreviewLocation(parsed, req.Reference.Location)
	return parsed.String(), nil
}

func appendWebPreviewLocation(target *url.URL, location PreviewLocation) {
	if target == nil || !location.Valid() {
		return
	}
	query := target.Query()
	query.Set("loc", location.QueryValue())
	target.RawQuery = query.Encode()
	target.Fragment = location.FragmentID()
}

func webPreviewGrantRequest(req FinalBlockPreviewRequest, scopePublicID string) WebPreviewGrantRequest {
	grantKey := strings.TrimSpace(req.PreviewGrantKey)
	if grantKey == "" {
		grantKey = fallbackWebPreviewGrantKey(req, scopePublicID)
	}
	return WebPreviewGrantRequest{
		ScopePublicID: strings.TrimSpace(scopePublicID),
		GrantKey:      grantKey,
	}
}

func fallbackWebPreviewGrantKey(req FinalBlockPreviewRequest, scopePublicID string) string {
	parts := []string{
		strings.TrimSpace(scopePublicID),
		strings.TrimSpace(req.GatewayID),
		strings.TrimSpace(req.SurfaceSessionID),
		strings.TrimSpace(req.Block.ThreadID),
		strings.TrimSpace(req.Block.TurnID),
		strings.TrimSpace(req.Block.ItemID),
		strings.TrimSpace(req.Block.ID),
	}
	for _, part := range parts[3:] {
		if part != "" {
			return "message|" + strings.Join(parts, "|")
		}
	}
	return "message|fallback|" + shortStablePreviewID(strings.Join(append(parts, strings.TrimSpace(req.Block.Text)), "|"))
}

func (p *DriveMarkdownPreviewer) loadWebPreviewArtifact(scopePublicID, previewID string) (*webPreviewArtifact, error) {
	if p == nil || strings.TrimSpace(scopePublicID) == "" || strings.TrimSpace(previewID) == "" {
		return nil, os.ErrNotExist
	}
	manifest, err := p.loadWebPreviewScopeManifest(scopePublicID)
	if err != nil {
		return nil, err
	}
	if manifest == nil || manifest.ScopePublicID != scopePublicID {
		return nil, os.ErrNotExist
	}
	record := manifest.Records[strings.TrimSpace(previewID)]
	if record == nil {
		return nil, os.ErrNotExist
	}
	now := p.nowFn().UTC()
	if !record.ExpiresAt.IsZero() && record.ExpiresAt.Before(now) {
		return nil, errPreviewRecordExpired
	}
	content, err := os.ReadFile(p.previewBlobPath(record.BlobKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errPreviewArtifactExpired
		}
		return nil, err
	}
	return &webPreviewArtifact{
		ScopePublicID: scopePublicID,
		PreviewID:     previewID,
		Record:        *record,
		Content:       content,
	}, nil
}

func (p *DriveMarkdownPreviewer) loadWebPreviewScopeManifest(scopePublicID string) (*webPreviewScopeManifest, error) {
	path := p.previewScopeManifestPath(scopePublicID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifest webPreviewScopeManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest.Records == nil {
		manifest.Records = map[string]*webPreviewRecord{}
	}
	return &manifest, nil
}

func (p *DriveMarkdownPreviewer) saveWebPreviewScopeManifest(manifest *webPreviewScopeManifest) error {
	if p == nil || manifest == nil || strings.TrimSpace(manifest.ScopePublicID) == "" {
		return nil
	}
	path := p.previewScopeManifestPath(manifest.ScopePublicID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func (p *DriveMarkdownPreviewer) ensurePreviewBlob(blobKey string, content []byte) error {
	blobPath := p.previewBlobPath(blobKey)
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		return err
	}
	tempPath := blobPath + ".tmp"
	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, blobPath); err != nil {
		if os.IsExist(err) {
			_ = os.Remove(tempPath)
			return nil
		}
		return err
	}
	return nil
}

func (p *DriveMarkdownPreviewer) previewScopeManifestPath(scopePublicID string) string {
	return filepath.Join(p.config.CacheDir, "scopes", scopePublicID+".json")
}

func (p *DriveMarkdownPreviewer) previewBlobPath(blobKey string) string {
	blobKey = strings.TrimSpace(blobKey)
	prefix := "00"
	if len(blobKey) >= 2 {
		prefix = blobKey[:2]
	}
	return filepath.Join(p.config.CacheDir, "blobs", "sha256", prefix, blobKey+".bin")
}
