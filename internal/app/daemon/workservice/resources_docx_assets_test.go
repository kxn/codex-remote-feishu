package workservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestDocxSnapshotSavesActualMediaBytesAndHash(t *testing.T) {
	image := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 32)...)
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/docx/v1/documents/doc":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"doc","revision_id":7}}}`)
		case "/open-apis/docx/v1/documents/doc/blocks":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"image","block_type":27,"image":{"token":"actual-media-token"}}],"has_more":false}}`)
		case "/open-apis/drive/v1/medias/actual-media-token/download":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(image)
		default:
			t.Errorf("unexpected SDK path%s", r.URL.Path)
		}
	})
	captured, err := connector.captureDocument(t.Context(), work.Resource{SourceIdentity: "docx:doc"}, "doc", CaptureResourceRequest{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Assets []SavedDocumentAsset `json:"assets"`
	}
	if err = json.Unmarshal(captured.Content, &saved); err != nil {
		t.Fatal(err)
	}
	if !captured.Complete || captured.Kind != work.VersionObservedDataset || len(saved.Assets) != 1 || !bytes.Equal(saved.Assets[0].Content, image) || saved.Assets[0].SHA256 == "" {
		t.Fatalf("not independent snapshot %+v %+v", captured, saved)
	}
}
func TestDocxMissingMediaAndForeignEmbedCannotClaimCompleteSnapshot(t *testing.T) {
	connector := platformConnector(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/docx/v1/documents/doc":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"document":{"document_id":"doc","revision_id":7}}}`)
		case "/open-apis/docx/v1/documents/doc/blocks":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"block_id":"image","block_type":27,"image":{"token":"missing-media"}},{"block_id":"sheet","sheet":{"token":"external-sheet"}}],"has_more":false}}`)
		case "/open-apis/drive/v1/medias/missing-media/download":
			w.WriteHeader(403)
			_, _ = fmt.Fprint(w, `{"code":99991672,"msg":"denied"}`)
		default:
			t.Errorf("unexpected SDK path%s", r.URL.Path)
		}
	})
	captured, err := connector.captureDocument(t.Context(), work.Resource{SourceIdentity: "docx:doc"}, "doc", CaptureResourceRequest{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Issues []string `json:"issues"`
		Blocks []any    `json:"blocks"`
	}
	if err = json.Unmarshal(captured.Content, &saved); err != nil {
		t.Fatal(err)
	}
	if captured.Complete || captured.Kind == work.VersionImmutableSnapshot || len(saved.Issues) != 2 || len(saved.Blocks) != 2 {
		t.Fatalf("partial content falsely complete %+v %+v", captured, saved)
	}
}
