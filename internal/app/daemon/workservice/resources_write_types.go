package workservice

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ResourceListRequest struct {
	Limit           int    `json:"limit,omitempty"`
	After           string `json:"after,omitempty"`
	DirectoriesOnly bool   `json:"directories_only,omitempty"`
}
type CreateResourceDocumentRequest struct {
	Key                    string `json:"key"`
	ParentResourceID       string `json:"parent_resource_id"`
	ExpectedParentRevision int64  `json:"expected_parent_revision"`
	Title                  string `json:"title"`
	Purpose                string `json:"purpose"`
	CollectionID           string `json:"collection_id"`
}
type InspectResourceDocumentRequest struct {
	ResourceID       string `json:"resource_id"`
	DocumentRevision int    `json:"document_revision,omitempty"`
	BlockID          string `json:"block_id,omitempty"`
	PageToken        string `json:"page_token,omitempty"`
	PageSize         int    `json:"page_size,omitempty"`
	TextRunIndex     *int   `json:"text_run_index,omitempty"`
	Offset           int    `json:"offset,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}
type AppendResourceDocumentRequest struct {
	Key                      string   `json:"key"`
	ResourceID               string   `json:"resource_id"`
	ExpectedResourceRevision int64    `json:"expected_resource_revision"`
	ObservedDocumentRevision int      `json:"observed_document_revision"`
	ParentBlockID            string   `json:"parent_block_id"`
	Index                    *int     `json:"index,omitempty"`
	Paragraphs               []string `json:"paragraphs"`
}
type ResourceTextRunEdit struct {
	BlockID             string `json:"block_id"`
	TextRunIndex        int    `json:"text_run_index"`
	ExpectedBlockSHA256 string `json:"expected_block_sha256"`
	Text                string `json:"text"`
}
type UpdateResourceDocumentRequest struct {
	Key                      string                `json:"key"`
	ResourceID               string                `json:"resource_id"`
	ExpectedResourceRevision int64                 `json:"expected_resource_revision"`
	ObservedDocumentRevision int                   `json:"observed_document_revision"`
	Edits                    []ResourceTextRunEdit `json:"edits"`
}
type ReadResourceSheetRequest struct {
	ResourceID string `json:"resource_id"`
	SheetID    string `json:"sheet_id"`
	Range      string `json:"range"`
}
type WriteResourceSheetRequest struct {
	Key                      string  `json:"key"`
	ResourceID               string  `json:"resource_id"`
	ExpectedResourceRevision int64   `json:"expected_resource_revision"`
	SheetID                  string  `json:"sheet_id"`
	Range                    string  `json:"range"`
	ExpectedDataSHA256       string  `json:"expected_data_sha256"`
	Values                   [][]any `json:"values"`
}
type ResourceDocumentRun struct {
	Index      int    `json:"index"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
	Characters int    `json:"characters"`
}
type ResourceDocumentBlock struct {
	ID         string                `json:"id"`
	Kind       int                   `json:"kind"`
	SHA256     string                `json:"sha256"`
	ChildCount int                   `json:"child_count"`
	RunCount   int                   `json:"run_count"`
	Runs       []ResourceDocumentRun `json:"runs,omitempty"`
	Text       *ContentPage          `json:"text,omitempty"`
}
type ResourceDocumentInspection struct {
	DocumentID        string                  `json:"document_id"`
	ObservedRevision  int                     `json:"observed_revision"`
	RequestedRevision int                     `json:"requested_revision"`
	Blocks            []ResourceDocumentBlock `json:"blocks"`
	HasMore           bool                    `json:"has_more"`
	NextPageToken     string                  `json:"next_page_token,omitempty"`
}
type ResourceSheetInspection struct {
	Values     feishu.ProjectSheetValues `json:"values"`
	DataSHA256 string                    `json:"data_sha256"`
}
type ResourceWriteReceipt struct {
	Attempted        bool            `json:"attempted"`
	ExternalID       string          `json:"external_id,omitempty"`
	SourceIdentity   string          `json:"source_identity,omitempty"`
	CanonicalURI     string          `json:"canonical_uri,omitempty"`
	DocumentRevision *int            `json:"document_revision,omitempty"`
	BlockIDs         []string        `json:"block_ids,omitempty"`
	RawEvidence      json.RawMessage `json:"raw_evidence,omitempty"`
}
type ResourceWriteResult struct {
	RecoveryOperationID string `json:"recovery_operation_id,omitempty"`
	Result
	Receipt          ResourceWriteReceipt `json:"receipt"`
	SnapshotSaved    bool                 `json:"snapshot_saved"`
	ReadbackVerified bool                 `json:"readback_verified"`
}
type ResourceDirectoryResolver interface {
	ResolveDirectory(context.Context, string) (ResolvedResource, error)
}
type ResourceWriteConnector interface {
	CreateResourceDocument(context.Context, work.Resource, CreateResourceDocumentRequest) (ResourceWriteReceipt, error)
	InspectResourceDocument(context.Context, work.Resource, InspectResourceDocumentRequest) (ResourceDocumentInspection, error)
	AppendResourceDocument(context.Context, work.Resource, AppendResourceDocumentRequest, string) (ResourceWriteReceipt, error)
	UpdateResourceDocument(context.Context, work.Resource, UpdateResourceDocumentRequest, string) (ResourceWriteReceipt, error)
	ReadResourceSheet(context.Context, work.Resource, ReadResourceSheetRequest) (ResourceSheetInspection, error)
	WriteResourceSheet(context.Context, work.Resource, WriteResourceSheetRequest) (ResourceWriteReceipt, error)
}
