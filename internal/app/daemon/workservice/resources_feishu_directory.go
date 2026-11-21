package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *FeishuResourceConnector) ResolveDirectory(ctx context.Context, link string) (ResolvedResource, error) {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ResolvedResource{}, fmt.Errorf("an actual HTTPS Drive folder or Wiki parent URL is required")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".feishu.cn") && !strings.HasSuffix(host, ".larksuite.com") && !strings.HasSuffix(host, ".larkoffice.com") {
		return ResolvedResource{}, fmt.Errorf("unrecognized Feishu document host")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	identity := ""
	var metadata any
	switch {
	case len(parts) == 3 && parts[0] == "drive" && parts[1] == "folder":
		if err = c.documents.CheckProjectFolder(ctx, parts[2]); err != nil {
			return ResolvedResource{}, err
		}
		identity = "folder:" + parts[2]
		metadata = map[string]any{"folder_token": parts[2], "sdk_access_verified_at": time.Now().UTC()}
	case len(parts) == 3 && parts[0] == "wiki" && parts[1] == "space":
		space, err := c.documents.GetWikiSpace(ctx, parts[2])
		if err != nil {
			return ResolvedResource{}, err
		}
		identity = "wiki_parent:" + space.ID + ":"
		metadata = space
	case len(parts) == 2 && parts[0] == "wiki":
		node, err := c.documents.ResolveWikiNode(ctx, feishu.ProjectWikiNodeToken(parts[1]))
		if err != nil {
			return ResolvedResource{}, err
		}
		identity = "wiki_parent:" + node.SpaceID + ":" + string(node.NodeToken)
		metadata = node
	default:
		return ResolvedResource{}, fmt.Errorf("directory must be an existing Drive folder, Wiki node or explicitly selected space root")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	source, evidence, err := observedSource("feishu_directory", identity, metadata)
	if err != nil {
		return ResolvedResource{}, err
	}
	return ResolvedResource{SourceIdentity: identity, CanonicalURI: parsed.String(), Source: source, Evidence: evidence}, nil
}
func resourceDirectory(v work.Resource) bool {
	return strings.HasPrefix(v.SourceIdentity, "folder:") || strings.HasPrefix(v.SourceIdentity, "wiki_parent:")
}
func (c *FeishuResourceConnector) CreateResourceDocument(ctx context.Context, parent work.Resource, req CreateResourceDocumentRequest) (ResourceWriteReceipt, error) {
	result := ResourceWriteReceipt{}
	base, err := url.Parse(parent.CanonicalURI)
	if err != nil {
		return result, err
	}
	if strings.HasPrefix(parent.SourceIdentity, "folder:") {
		token := strings.TrimPrefix(parent.SourceIdentity, "folder:")
		result.Attempted = true
		created, err := c.documents.CreateDocument(ctx, feishu.ProjectCreateDocumentRequest{Title: req.Title, FolderToken: token})
		result.ExternalID = string(created.DocumentID)
		result.SourceIdentity = "docx:" + result.ExternalID
		result.DocumentRevision = created.RevisionID
		result.RawEvidence, _ = json.Marshal(created)
		if result.ExternalID != "" {
			base.Path = "/docx/" + result.ExternalID
			base.RawQuery = ""
			base.Fragment = ""
			result.CanonicalURI = base.String()
		}
		return result, err
	}
	if strings.HasPrefix(parent.SourceIdentity, "wiki_parent:") {
		parts := strings.SplitN(strings.TrimPrefix(parent.SourceIdentity, "wiki_parent:"), ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return result, fmt.Errorf("registered Wiki parent identity malformed")
		}
		result.Attempted = true
		created, err := c.documents.CreateWikiNode(ctx, feishu.ProjectCreateWikiNodeRequest{SpaceID: parts[0], ParentNodeToken: feishu.ProjectWikiNodeToken(parts[1]), AtSpaceRoot: parts[1] == "", ObjType: "docx", Title: req.Title})
		result.ExternalID = created.ObjToken
		result.SourceIdentity = "docx:" + created.ObjToken
		result.CanonicalURI = created.URL
		result.RawEvidence, _ = json.Marshal(created)
		if result.CanonicalURI == "" && created.NodeToken != "" {
			base.Path = "/wiki/" + string(created.NodeToken)
			base.RawQuery = ""
			base.Fragment = ""
			result.CanonicalURI = base.String()
		}
		return result, err
	}
	return result, fmt.Errorf("parent resource is not a registered directory")
}
