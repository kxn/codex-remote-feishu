package feishu

import (
	"context"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
)

type ProjectDocumentMoveReceipt struct {
	DocumentID string `json:"document_id"`
	NodeToken  string `json:"node_token,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	RequestID  string `json:"request_id"`
}

func (a *ProjectDocumentAPI) MoveDocumentToFolder(ctx context.Context, documentID, folderToken string) (ProjectDocumentMoveReceipt, error) {
	result := ProjectDocumentMoveReceipt{DocumentID: documentID}
	for _, value := range []string{documentID, folderToken} {
		if err := projectBareToken(value, "move identity"); err != nil {
			return result, err
		}
	}
	const api = "drive.v1.file.move"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, documentID, false), func(ctx context.Context, c *lark.Client) (*larkdrive.MoveFileResp, error) {
		resp, err := c.Drive.V1.File.Move(ctx, larkdrive.NewMoveFileReqBuilder().FileToken(documentID).Body(&larkdrive.MoveFileReqBody{Type: projectString("docx"), FolderToken: &folderToken}).Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("move returned nil response")
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if resp != nil {
		result.RequestID = resp.RequestId()
		if resp.Data != nil {
			result.TaskID = scopeStringValue(resp.Data.TaskId)
		}
	}
	return result, err
}
func projectString(value string) *string { return &value }
func (a *ProjectDocumentAPI) DocumentInFolder(ctx context.Context, documentID, folder string) (bool, error) {
	for _, value := range []string{documentID, folder} {
		if err := projectBareToken(value, "folder observation identity"); err != nil {
			return false, err
		}
	}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		const api = "drive.v1.file.list"
		resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, folder, true), func(ctx context.Context, c *lark.Client) (*larkdrive.ListFileResp, error) {
			b := larkdrive.NewListFileReqBuilder().FolderToken(folder).PageSize(200)
			if cursor != "" {
				b.PageToken(cursor)
			}
			resp, err := c.Drive.V1.File.List(ctx, b.Build())
			if err != nil {
				return resp, err
			}
			if resp == nil {
				return nil, fmt.Errorf("folder list returned nil")
			}
			if !resp.Success() {
				return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
			}
			if resp.Data == nil {
				return resp, fmt.Errorf("folder list returned no data")
			}
			return resp, nil
		})
		if err != nil {
			return false, err
		}
		for _, file := range resp.Data.Files {
			if file != nil && scopeStringValue(file.Token) == documentID && scopeStringValue(file.Type) == "docx" && (file.ParentToken == nil || scopeStringValue(file.ParentToken) == folder) {
				return true, nil
			}
		}
		if resp.Data.HasMore == nil || !*resp.Data.HasMore {
			return false, nil
		}
		cursor = scopeStringValue(resp.Data.NextPageToken)
		if cursor == "" || seen[cursor] {
			return false, fmt.Errorf("folder observation has invalid continuation")
		}
		seen[cursor] = true
	}
	return false, fmt.Errorf("folder observation exceeded100 pages; absence not established")
}

// WikiNodeForDocument resolves the actual object token through obj_type=docx;
// it never confuses that identity with a wiki node token.
func (a *ProjectDocumentAPI) WikiNodeForDocument(ctx context.Context, id string) (ProjectWikiNode, error) {
	if err := projectBareToken(id, "document id"); err != nil {
		return ProjectWikiNode{}, err
	}
	const api = "wiki.v2.space.get_node"
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, id, true), func(ctx context.Context, c *lark.Client) (*larkwiki.GetNodeSpaceResp, error) {
		resp, err := c.Wiki.V2.Space.GetNode(ctx, larkwiki.NewGetNodeSpaceReqBuilder().Token(id).ObjType("docx").Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("Wiki object resolution returned nil")
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		if resp.Data == nil {
			return resp, fmt.Errorf("Wiki object resolution returned no data")
		}
		return resp, nil
	})
	if err != nil {
		return ProjectWikiNode{}, err
	}
	node, err := projectWikiNode(resp.Data.Node, resp.RequestId())
	if err == nil && (node.ObjType != "docx" || node.ObjToken != id) {
		return node, fmt.Errorf("Wiki object resolution returned another content identity")
	}
	return node, err
}
func (a *ProjectDocumentAPI) MoveDocumentToWiki(ctx context.Context, id, sourceSpace, sourceNode, targetSpace, targetParent string) (ProjectDocumentMoveReceipt, error) {
	result := ProjectDocumentMoveReceipt{DocumentID: id}
	for _, value := range []string{id, targetSpace} {
		if err := projectBareToken(value, "Wiki move identity"); err != nil {
			return result, err
		}
	}
	if targetParent != "" {
		if err := projectBareToken(targetParent, "Wiki target parent"); err != nil {
			return result, err
		}
	}
	if sourceNode != "" {
		if err := projectBareToken(sourceSpace, "source Wiki space"); err != nil {
			return result, err
		}
		if err := projectBareToken(sourceNode, "source Wiki node"); err != nil {
			return result, err
		}
		const api = "wiki.v2.space_node.move"
		resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, id, false), func(ctx context.Context, c *lark.Client) (*larkwiki.MoveSpaceNodeResp, error) {
			resp, err := c.Wiki.V2.SpaceNode.Move(ctx, larkwiki.NewMoveSpaceNodeReqBuilder().SpaceId(sourceSpace).NodeToken(sourceNode).Body(&larkwiki.MoveSpaceNodeReqBody{TargetSpaceId: &targetSpace, TargetParentToken: &targetParent}).Build())
			if err != nil {
				return resp, err
			}
			if resp == nil {
				return nil, fmt.Errorf("Wiki move returned nil")
			}
			if !resp.Success() {
				return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
			}
			return resp, nil
		})
		if resp != nil {
			result.RequestID = resp.RequestId()
			if resp.Data != nil && resp.Data.Node != nil {
				result.NodeToken = scopeStringValue(resp.Data.Node.NodeToken)
			}
		}
		return result, err
	}
	const api = "wiki.v2.space_node.move_docs_to_wiki"
	apply := false
	resp, err := DoSDK(ctx, a.broker, projectResourceSpec(api, id, false), func(ctx context.Context, c *lark.Client) (*larkwiki.MoveDocsToWikiSpaceNodeResp, error) {
		resp, err := c.Wiki.V2.SpaceNode.MoveDocsToWiki(ctx, larkwiki.NewMoveDocsToWikiSpaceNodeReqBuilder().SpaceId(targetSpace).Body(&larkwiki.MoveDocsToWikiSpaceNodeReqBody{ParentWikiToken: &targetParent, ObjType: projectString("docx"), ObjToken: &id, Apply: &apply}).Build())
		if err != nil {
			return resp, err
		}
		if resp == nil {
			return nil, fmt.Errorf("Wiki import returned nil")
		}
		if !resp.Success() {
			return resp, newAPIError(api, resp.ApiResp, resp.CodeError)
		}
		return resp, nil
	})
	if resp != nil {
		result.RequestID = resp.RequestId()
		if resp.Data != nil {
			result.NodeToken = scopeStringValue(resp.Data.WikiToken)
			result.TaskID = scopeStringValue(resp.Data.TaskId)
			if resp.Data.Applied != nil && *resp.Data.Applied {
				return result, fmt.Errorf("Wiki move unexpectedly requested a permission approval")
			}
		}
	}
	return result, err
}
