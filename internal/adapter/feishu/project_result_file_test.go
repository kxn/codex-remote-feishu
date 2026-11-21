package feishu

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResultFileUploadAndPublishUseSeparateActualReceipts(t *testing.T) {
	calls := 0
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.URL.Path != "/open-apis/im/v1/files" {
				t.Errorf("upload path%s", r.URL.Path)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("file_type") != "stream" || r.FormValue("file_name") != "result.txt" {
				t.Errorf("multipart fields %#v", r.MultipartForm.Value)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if string(data) != "actual output" {
				t.Fatal("missing uploaded bytes")
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"file_key":"real-file-key"}}`)
		} else {
			body := projectReadJSON(t, r)
			if r.URL.Query().Get("receive_id_type") != "chat_id" || body["msg_type"] != "file" || body["uuid"] != "persistent-uuid" || !strings.Contains(body["content"].(string), "real-file-key") {
				t.Errorf("publish%+v", body)
			}
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"message_id":"actual-sent-message","chat_id":"chat"}}`)
		}
	})
	key, err := api.UploadResultFile(t.Context(), UploadResultFileRequest{FileName: "result.txt", Content: []byte("actual output")})
	if err != nil || key != "real-file-key" || calls != 1 {
		t.Fatalf("upload%s %v calls%d", key, err, calls)
	}
	receipt, err := api.PublishResultFile(t.Context(), PublishResultFileRequest{ChatID: "chat", FileKey: key, UUID: "persistent-uuid"})
	if err != nil || receipt.MessageID != "actual-sent-message" {
		t.Fatalf("publish%+v %v", receipt, err)
	}
}
func TestResultFileUploadFailureNeverRetries(t *testing.T) {
	calls := 0
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		_, _ = fmt.Fprint(w, `{"code":999,"msg":"failed"}`)
	})
	if _, err := api.UploadResultFile(t.Context(), UploadResultFileRequest{FileName: "result.txt", Content: []byte("bytes")}); err == nil || calls != 1 {
		t.Fatalf("err%v calls%d", err, calls)
	}
	if _, err := api.UploadResultFile(t.Context(), UploadResultFileRequest{FileName: "empty.txt"}); err == nil || calls != 1 {
		t.Fatal("invalid upload executed")
	}
}
