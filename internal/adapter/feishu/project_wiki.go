package feishu

import (
	"context"
	"fmt"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
)

type ProjectWikiNode struct {
	NodeToken            ProjectWikiNodeToken
	ObjToken             string
	ObjType              string
	SpaceID              string
	ParentNodeToken      ProjectWikiNodeToken
	NodeType, Title, URL string
	OriginNodeToken      ProjectWikiNodeToken
	OriginSpaceID        string
	RequestID            string
}

// DocxDocumentID converts the resolved content identity, never the navigation
// node token. Sheet/Bitable nodes must use their corresponding connectors.
func (n ProjectWikiNode) DocxDocumentID() (ProjectDocumentID, error) {
	if n.ObjType != "docx" {
		return "", fmt.Errorf("wiki object type %q is not docx", n.ObjType)
	}
	if err := projectBareToken(n.ObjToken, "wiki docx object token"); err != nil {
		return "", err
	}
	return ProjectDocumentID(n.ObjToken), nil
}

type ProjectCreateWikiNodeRequest struct {
	SpaceID         string
	ParentNodeToken ProjectWikiNodeToken
	AtSpaceRoot     bool   // Explicit selection of an existing space root, not space creation.
	ObjType         string // This product creates docx, sheet, or bitable origin nodes.
	Title           string
}

func projectWikiNode(node *larkwiki.Node, requestID string) (ProjectWikiNode, error) {
	result := ProjectWikiNode{RequestID: requestID}
	if node == nil {
		return result, fmt.Errorf("missing wiki node")
	}
	result.NodeToken = ProjectWikiNodeToken(scopeStringValue(node.NodeToken))
	result.ObjToken = scopeStringValue(node.ObjToken)
	result.ObjType = scopeStringValue(node.ObjType)
	result.SpaceID = scopeStringValue(node.SpaceId)
	result.ParentNodeToken = ProjectWikiNodeToken(scopeStringValue(node.ParentNodeToken))
	result.NodeType = scopeStringValue(node.NodeType)
	result.Title = scopeStringValue(node.Title)
	result.URL = scopeStringValue(node.Url)
	result.OriginNodeToken = ProjectWikiNodeToken(scopeStringValue(node.OriginNodeToken))
	result.OriginSpaceID = scopeStringValue(node.OriginSpaceId)
	if result.NodeToken == "" || result.ObjToken == "" || result.ObjType == "" || result.SpaceID == "" {
		return result, fmt.Errorf("incomplete wiki node identity")
	}
	return result, nil
}

func (a *ProjectDocumentAPI) ResolveWikiNode(ctx context.Context, nodeToken ProjectWikiNodeToken) (ProjectWikiNode, error) {
	if err := projectBareToken(string(nodeToken), "wiki node token"); err != nil {
		return ProjectWikiNode{}, err
	}
	const api = "wiki.v2.space.get_node"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, string(nodeToken), true), func(ctx context.Context, c *lark.Client) (*larkwiki.GetNodeSpaceResp, error) {
		// obj_type=wiki is explicit: this method accepts navigation tokens only.
		resp, err := c.Wiki.V2.Space.GetNode(ctx, larkwiki.NewGetNodeSpaceReqBuilder().Token(string(nodeToken)).ObjType("wiki").Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if resp == nil {
		return ProjectWikiNode{}, err
	}
	if resp.Data == nil {
		return ProjectWikiNode{RequestID: resp.RequestId()}, err
	}
	result, parseErr := projectWikiNode(resp.Data.Node, resp.RequestId())
	if err != nil {
		return result, err
	}
	return result, parseErr
}

// CreateWikiNode requires an existing, explicitly selected parent or space root.
// The tenant bot needs create permission there. It cannot create a Wiki space,
// and a readable/editable child does not establish parent-container permission.
func (a *ProjectDocumentAPI) CreateWikiNode(ctx context.Context, req ProjectCreateWikiNodeRequest) (ProjectWikiNode, error) {
	if err := projectBareToken(req.SpaceID, "wiki space id"); err != nil {
		return ProjectWikiNode{}, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return ProjectWikiNode{}, fmt.Errorf("missing wiki title")
	}
	if req.AtSpaceRoot {
		if req.ParentNodeToken != "" {
			return ProjectWikiNode{}, fmt.Errorf("space root and parent node are mutually exclusive")
		}
	} else if err := projectBareToken(string(req.ParentNodeToken), "parent wiki node token"); err != nil {
		return ProjectWikiNode{}, err
	}
	switch req.ObjType {
	case "docx", "sheet", "bitable":
	default:
		return ProjectWikiNode{}, fmt.Errorf("unsupported project wiki object type %q", req.ObjType)
	}
	nodeType := "origin"
	node := &larkwiki.Node{ObjType: &req.ObjType, NodeType: &nodeType, Title: &req.Title}
	if !req.AtSpaceRoot {
		parent := string(req.ParentNodeToken)
		node.ParentNodeToken = &parent
	}
	const api = "wiki.v2.space.node.create"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, req.SpaceID, false), func(ctx context.Context, c *lark.Client) (*larkwiki.CreateSpaceNodeResp, error) {
		resp, err := c.Wiki.V2.SpaceNode.Create(ctx, larkwiki.NewCreateSpaceNodeReqBuilder().SpaceId(req.SpaceID).Node(node).Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%s returned nil response", api)
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("%s returned nil data", api)
		}
		return resp, nil
	})
	if resp == nil {
		return ProjectWikiNode{}, err
	}
	if resp.Data == nil {
		return ProjectWikiNode{RequestID: resp.RequestId()}, err
	}
	result, parseErr := projectWikiNode(resp.Data.Node, resp.RequestId())
	if err != nil {
		return result, err
	}
	return result, parseErr
}
