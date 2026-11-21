package feishu

import (
	"fmt"
	"strings"

	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
)

// Distinct token types keep Wiki navigation identity separate from Docx content.
type ProjectDocumentID string
type ProjectWikiNodeToken string

type ProjectDocumentAPI struct{ broker *FeishuCallBroker }

func NewProjectDocumentAPI(broker *FeishuCallBroker) *ProjectDocumentAPI {
	return &ProjectDocumentAPI{broker: broker}
}
func (g *LiveGateway) ProjectDocumentAPI() *ProjectDocumentAPI {
	if g == nil {
		return nil
	}
	return NewProjectDocumentAPI(g.broker)
}

type ProjectCreateDocumentRequest struct{ Title, FolderToken string }

// Metadata may contain partial response evidence when err != nil; callers must
// check the error before treating a returned identity as a successful creation.
type ProjectDocumentMetadata struct {
	DocumentID ProjectDocumentID
	Title      string
	RevisionID *int
	RequestID  string
}

type ProjectDocumentBlocksRequest struct {
	DocumentID ProjectDocumentID
	RevisionID int // -1 latest, or >=1 historical; never an expected-current CAS.
	PageToken  string
	PageSize   int
}

type ProjectDocumentBlocksPage struct {
	DocumentID          ProjectDocumentID
	RequestedRevisionID int // List returns no observed revision; -1 is not a snapshot.
	Blocks              []*larkdocx.Block
	HasMore             bool
	NextPageToken       string
	RequestID           string
	// This is one page only. Even HasMore=false does not prove earlier pages read.
}

type ProjectAppendDocumentBlocksRequest struct {
	DocumentID    ProjectDocumentID
	ParentBlockID string
	RevisionID    int
	Index         int    // -1 append; 0 inserts first. Explicit platform position.
	ClientToken   string // Only these block mutation endpoints expose this capability.
	Blocks        []*larkdocx.Block
}

type ProjectUpdateDocumentBlocksRequest struct {
	DocumentID  ProjectDocumentID
	RevisionID  int
	ClientToken string
	Requests    []*larkdocx.UpdateBlockRequest
}

// Mutation fields survive platform errors for reconciliation. They do not turn
// an errored or uncertain write into success.
type ProjectDocumentBlocksMutation struct {
	DocumentID  ProjectDocumentID
	Blocks      []*larkdocx.Block
	RevisionID  *int
	ClientToken string
	RequestID   string
	// A response receipt is not a read-back, a durable version, or a CAS guarantee.
}

func projectResourceSpec(api, token string, read bool) CallSpec {
	spec := CallSpec{API: api, Class: CallClassDrive, Priority: CallPriorityInteractive,
		ResourceKey: FeishuResourceKey{DocToken: token}, Retry: RetryOff, Permission: PermissionFailFast}
	if read {
		spec.Priority = CallPriorityReadAssist
		spec.Retry = RetrySafe
	}
	return spec
}

// APIs take bare tokens, never URLs or path/query fragments. Prefix guessing is
// deliberately avoided: Wiki resolution must inspect ObjType before conversion.
func projectBareToken(value, kind string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "/\\?# !:") {
		return fmt.Errorf("invalid %s: expected bare token", kind)
	}
	return nil
}

func projectDocumentRevision(id ProjectDocumentID, revision int) error {
	if err := projectBareToken(string(id), "document id"); err != nil {
		return err
	}
	if revision != -1 && revision < 1 {
		return fmt.Errorf("document revision must be -1 or >=1")
	}
	return nil
}

func projectDocumentMetadata(doc *larkdocx.Document, requestID string) (ProjectDocumentMetadata, error) {
	if doc == nil || doc.DocumentId == nil || *doc.DocumentId == "" {
		return ProjectDocumentMetadata{RequestID: requestID}, fmt.Errorf("missing document identity")
	}
	result := ProjectDocumentMetadata{DocumentID: ProjectDocumentID(*doc.DocumentId), Title: scopeStringValue(doc.Title), RevisionID: doc.RevisionId, RequestID: requestID}
	if doc.RevisionId == nil {
		return result, fmt.Errorf("missing document revision")
	}
	return result, nil
}
