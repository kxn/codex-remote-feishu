package feishu

import (
	"fmt"
	"net/http"
	"testing"
)

func TestProjectContactIdentityChecksExactOpenID(t *testing.T) {
	returned := "ou_user"
	api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/open-apis/contact/v3/users/ou_user" || r.URL.Query().Get("user_id_type") != "open_id" {
			t.Errorf("unexpected contact lookup: %s %s", r.Method, r.URL.String())
		}
		_, _ = fmt.Fprintf(w, `{"code":0,"data":{"user":{"open_id":%q,"user_id":"tenant-user"}}}`, returned)
	})
	id, err := api.ResolveContactUserID(t.Context(), "ou_user")
	if err != nil || id != "tenant-user" {
		t.Fatalf("contact ID: %q %v", id, err)
	}
	returned = "ou_other"
	if _, err = api.ResolveContactUserID(t.Context(), "ou_user"); err == nil {
		t.Fatal("accepted mismatching contact identity")
	}
}
