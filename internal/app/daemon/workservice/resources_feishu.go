package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type FeishuResourceConnector struct {
	identity  string
	documents *feishu.ProjectDocumentAPI
	sheets    *feishu.ProjectSheetsAPI
	bitable   feishu.BitableAPI
}

func NewFeishuResourceConnector(identity string, gateway *feishu.LiveGateway) (*FeishuResourceConnector, error) {
	if identity == "" || gateway == nil {
		return nil, fmt.Errorf("explicit gateway identity is required")
	}
	return &FeishuResourceConnector{identity: identity, documents: gateway.ProjectDocumentAPI(), sheets: gateway.ProjectSheetsAPI(), bitable: gateway.ProjectBitableAPI()}, nil
}
func (c *FeishuResourceConnector) Identity() string { return c.identity }

func resourceLink(value string) (kind, token string, err error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", "", fmt.Errorf("an HTTPS cloud document URL is required")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return "", "", fmt.Errorf("resource link must use a recognized Feishu or Lark document host")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[1] == "" || strings.ContainsAny(parts[1], "/\\?# !:") {
		return "", "", fmt.Errorf("cloud document URL must identify one existing object")
	}
	switch parts[0] {
	case "docx", "wiki", "sheets", "base":
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("cloud document URL type is not docx, wiki, sheets or base")
}
func observedSource(kind, id string, evidence any) (work.SourceRef, json.RawMessage, error) {
	data, err := json.Marshal(evidence)
	if err != nil {
		return work.SourceRef{}, nil, err
	}
	sum := sha256.Sum256(data)
	source := work.SourceRef{Kind: kind, ID: id, Version: "observed-sha256:" + hex.EncodeToString(sum[:]), EvidenceRef: "platform-metadata"}
	return source, data, nil
}

func (c *FeishuResourceConnector) Resolve(ctx context.Context, link string) (ResolvedResource, error) {
	kind, token, err := resourceLink(link)
	if err != nil {
		return ResolvedResource{}, err
	}
	canonical := link
	var wiki *feishu.ProjectWikiNode
	if kind == "wiki" {
		node, err := c.documents.ResolveWikiNode(ctx, feishu.ProjectWikiNodeToken(token))
		if err != nil {
			return ResolvedResource{}, err
		}
		wiki = &node
		token = node.ObjToken
		switch node.ObjType {
		case "docx":
			kind = "docx"
		case "sheet":
			kind = "sheets"
		case "bitable":
			kind = "base"
		default:
			return ResolvedResource{}, fmt.Errorf("wiki node resolves to unsupported content type %q", node.ObjType)
		}
		if node.URL != "" {
			canonical = node.URL
		}
	}
	var metadata any
	var revision string
	switch kind {
	case "docx":
		doc, err := c.documents.GetDocument(ctx, feishu.ProjectDocumentID(token))
		if err != nil {
			return ResolvedResource{}, err
		}
		metadata = doc
		revision = strconv.Itoa(*doc.RevisionID)
	case "sheets":
		sheet, err := c.sheets.GetSpreadsheet(ctx, token)
		if err != nil {
			return ResolvedResource{}, err
		}
		metadata = sheet
	case "base":
		app, err := c.bitable.GetApp(ctx, token)
		if err != nil {
			return ResolvedResource{}, err
		}
		metadata = app
	}
	identity := kind + ":" + token
	source, evidence, err := observedSource("feishu_"+kind, identity, struct {
		Metadata any                     `json:"metadata"`
		Wiki     *feishu.ProjectWikiNode `json:"wiki,omitempty"`
	}{metadata, wiki})
	if err != nil {
		return ResolvedResource{}, err
	}
	if revision != "" {
		source.Version = revision
	}
	return ResolvedResource{SourceIdentity: identity, CanonicalURI: canonical, Source: source, Evidence: evidence}, nil
}

func (c *FeishuResourceConnector) Capture(ctx context.Context, resource work.Resource, req CaptureResourceRequest) (CapturedResource, error) {
	kind, token, ok := strings.Cut(resource.SourceIdentity, ":")
	if !ok || token == "" {
		return CapturedResource{}, fmt.Errorf("resource has no resolved platform object identity")
	}
	started := time.Now().UTC()
	switch kind {
	case "docx":
		return c.captureDocument(ctx, resource, token, req, started)
	case "sheets":
		return c.captureSheet(ctx, resource, token, req, started)
	case "base":
		return c.captureBitable(ctx, resource, token, req, started)
	default:
		return CapturedResource{}, fmt.Errorf("resource content type %q has no selected connector", kind)
	}
}

// FeishuConnectorIdentity fences application rebinding, not merely a gateway's
// display name. Registering under one app cannot silently borrow a new bot's rights.
func FeishuConnectorIdentity(binding work.RoleBotBinding) string {
	return "feishu:" + binding.GatewayRef + ":" + binding.AppID + ":" + strconv.FormatInt(binding.IdentityGeneration, 10)
}
