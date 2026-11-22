package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateFilesCreateTopLevelMessagesThroughSDK(t *testing.T) {
	for _, kind := range []string{"sendfile", "saved-artifact"} {
		t.Run(kind, func(t *testing.T) {
			a, source := teamPickerFixture(t, isolatedSDKFixtureAppID(t, kind))
			const payload = "saved private file bytes"
			sent := make(chan string, 2)
			platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/callback/ws/endpoint":
					http.NotFound(w, r)
				case "/open-apis/auth/v3/tenant_access_token/internal":
					fmt.Fprint(w, `{"code":0,"tenant_access_token":"private-file-token","expire":7200}`)
				case "/open-apis/im/v1/files":
					file, _, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						http.Error(w, "missing file", 400)
						return
					}
					defer file.Close()
					data, err := io.ReadAll(file)
					if err != nil || string(data) != payload || r.FormValue("file_name") != "report.txt" {
						t.Errorf("wrong frozen upload: %q %v", data, err)
					}
					fmt.Fprint(w, `{"code":0,"data":{"file_key":"private-file-key"}}`)
				case "/open-apis/im/v1/messages":
					var body struct {
						ReceiveID     string `json:"receive_id"`
						Kind          string `json:"msg_type"`
						Content       string `json:"content"`
						ReplyInThread *bool  `json:"reply_in_thread"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.Query().Get("receive_id_type") != "chat_id" || body.ReceiveID != source.ChatID || body.Kind != "file" || body.Content != `{"file_key":"private-file-key"}` || body.ReplyInThread != nil {
						t.Errorf("file must create a top-level private message: %s %s %+v", r.Method, r.URL, body)
					}
					sent <- body.ReceiveID
					fmt.Fprintf(w, `{"code":0,"data":{"message_id":"om_private_file","chat_id":%q}}`, source.ChatID)
				default:
					t.Errorf("unexpected private file route %s %s (old source %s)", r.Method, r.URL, source.MessageID)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(platform.Close)
			useDeliveryJourneyGateway(t, a, platform.URL, source.Gateway)
			if source.MessageID == "" {
				t.Fatal("fixture must retain its old private source")
			}
			if kind == "sendfile" {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(payload), 0600); err != nil {
					t.Fatal(err)
				}
				pickerTx(t, a, func(tx *workstore.Tx) error {
					if err := tx.PutLocation(work.ExecutionLocation{ID: "file-dir", HostID: "local", Cwd: dir, Generation: 1, Revision: 1}, 0); err != nil {
						return err
					}
					evidence := work.NewContent("file-dir-selection", source.ScopeID, "text/plain", []byte("human selected directory"), time.Now().UTC())
					if err := tx.PutContent(evidence); err != nil {
						return err
					}
					return tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: source.ScopeID, SelectedLocationIDs: []string{"file-dir"}, DefaultLocationID: "file-dir", SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: source.PrincipalID}, SourceRef: evidence.ID, Revision: 1}, 0)
				})
				action, known := parseTeamPrivateCommand("/sendfile report.txt")
				if !known {
					t.Fatal("literal /sendfile was not recognized")
				}
				if _, err := a.handleTeamSendFile(t.Context(), source, action); err != nil {
					t.Fatal(err)
				}
			} else {
				id, err := a.queueTeamPicker(t.Context(), source, "artifact-test", "saved", 1, workui.Panel{Title: "Saved file", Buttons: []workui.Button{{Label: "Download", Action: "artifacts.download", Values: map[string]any{"asset": 0}}}})
				if err != nil {
					t.Fatal(err)
				}
				pickerTx(t, a, func(tx *workstore.Tx) error {
					content := work.NewContent("saved-private-file", source.ScopeID, "text/plain", []byte(payload), time.Now().UTC())
					if err := tx.PutContent(content); err != nil {
						return err
					}
					resource := work.Resource{ID: "saved-resource", ScopeID: source.ScopeID, CollectionID: "files", ConnectorIdentity: work.SavedFileConnector, SourceIdentity: "saved-private-file", CanonicalURI: "relay://saved-private-file", Purpose: "Saved file", Revision: 1}
					if err := tx.PutResource(resource, 0); err != nil {
						return err
					}
					version := work.ResourceVersion{ID: "saved-version", ResourceID: resource.ID, ScopeID: source.ScopeID, Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "file", ID: resource.ID, Version: "1", EvidenceRef: content.ID}, ContentRef: content.ID, ContentHash: content.SHA256, FileName: "report.txt", Consistency: work.ConsistencyAtomic, Complete: true, Saved: true}
					if err := tx.PutResourceVersion(version); err != nil {
						return err
					}
					raw, err := json.Marshal(artifactCursor{References: []work.DeliveryRef{{ResourceID: resource.ID, VersionID: version.ID}}})
					if err != nil {
						return err
					}
					return tx.PutContent(work.NewContent(id+":artifact-cursor", source.ScopeID, "application/json", raw, time.Now().UTC()))
				})
				view, interaction := pickerAction(t, a, source, id, "artifacts.download")
				if interaction.Envelope.MessageID == "" {
					t.Fatal("download must retain the old card as callback authority")
				}
				if err := a.sendTeamSavedArtifactAsset(t.Context(), interaction, view); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case chat := <-sent:
				if chat != source.ChatID {
					t.Fatal(chat)
				}
			default:
				t.Fatal("no real SDK top-level file creation")
			}
		})
	}
}
