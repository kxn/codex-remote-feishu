package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestModelCatalogNativeCredentialChangeDuringQueryPreservesLastGood(t *testing.T) {
	f, key, binding := catalogAppFixture(t)
	seedProfileCatalog(t, f, key, time.Now())
	target, err := f.a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, "")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	auth := filepath.Join(home, "auth.json")
	if err = os.WriteFile(auth, []byte(`{"auth_mode":"chatgpt","tokens":{"account_id":"account-a","access_token":"old"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	target.NativeEnv = []string{"CODEX_HOME=" + home}
	target.NativeGuard, err = nativeCatalogCredentialGuard(target.NativeEnv, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.a.runCodexModelCatalogProbe = func(_ context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		if e := os.WriteFile(auth, []byte(`{"auth_mode":"chatgpt","tokens":{"account_id":"account-b","access_token":"new"}}`), 0600); e != nil {
			return codexprofile.ModelCatalogProbeObservation{}, e
		}
		return catalogProbeResult(options, "other-account-model"), nil
	}
	if _, err = f.a.readOrRefreshProfileCatalog(t.Context(), target, true); err != nil {
		t.Fatal(err)
	}
	f.a.profileCatalogWorkers.Wait()
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		result, e := tx.GetProfileModelCatalog(key)
		if e != nil {
			return e
		}
		if result.Refreshing || !strings.Contains(result.LastError, "发生变化") || result.Snapshot.Entries[0].Model != "last-good-model" {
			t.Fatalf("changed credential polluted cache: %+v", result)
		}
		return nil
	})
}
