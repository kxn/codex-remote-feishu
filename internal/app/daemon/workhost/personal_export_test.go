package workhost

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPersonalExportCapturesActualPreparedLaunch(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 1234, nil })
	if err := h.Prepare(context.Background(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}); err != nil {
		t.Fatal(err)
	}
	var snapshot workstore.PersonalExport
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		var err error
		snapshot, err = tx.CapturePersonalExport("scope", workstore.PersonalExportLimits{MaxRows: 1000, MaxBytes: 8 << 20})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range snapshot.Records {
		if record.Table != "content" {
			continue
		}
		var launch LaunchRecord
		if json.Unmarshal(record.Data, &launch) == nil && launch.ScopeID == "scope" && launch.WorkerID != "" {
			found = true
			if launch.Snapshot.CWD == "" || launch.Snapshot.Owner.ScopeID != "scope" || launch.Snapshot.InstructionBundle.Text == "" {
				t.Fatal("incomplete launch snapshot")
			}
			if strings.Contains(string(record.Data), "do-not-persist") {
				t.Fatal("launch leaked provider credentials")
			}
		}
	}
	if !found {
		t.Fatal("prepared launch record not exported")
	}
}
