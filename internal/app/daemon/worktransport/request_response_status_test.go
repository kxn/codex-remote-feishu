package worktransport

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestNativeResponseTransportStateUpdatesSameCardWithoutRequestRevision(t *testing.T) {
	f := responseIntegrationFixture(t, false)
	f.driver.BindRegistrar(failBeforeResponseRegistration{})
	id, err := f.service.Respond(t.Context(), f.decision)
	if err == nil {
		t.Fatal("fixture did not fail locally")
	}
	currentRevision := int64(0)
	var previousView string
	check := func(want string) {
		t.Helper()
		if err := f.service.ProjectForms(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.tx(func(tx *workstore.Tx) error {
			r, e := tx.GetNativeRequest(f.request.ID)
			if e != nil {
				return e
			}
			if currentRevision == 0 {
				currentRevision = r.Revision
			} else if r.Revision != currentRevision {
				t.Fatal("presentation changed business request revision")
			}
			v, e := tx.CurrentNativeRequestView(r.ScopeID, r.ID, "request:"+r.ID)
			if e != nil {
				return e
			}
			if previousView != "" && v.ID == previousView {
				t.Fatal("operation revision did not refresh request card")
			}
			op, e := tx.GetOperation(v.OperationID)
			if e != nil {
				return e
			}
			p, e := workservice.ReadMessageProjection(tx, op)
			if e != nil {
				return e
			}
			if p.ReplaceMessageID != "native-card" || !strings.Contains(string(p.Card), want) {
				t.Fatalf("wrong destination or status, wanted %q", want)
			}
			previousView = v.ID
			return nil
		})
		confirmNativeRequestView(f, previousView)
	}
	check("尚未送到后端")
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		due := time.Now().Add(-time.Second)
		op.NextAttemptAt = &due
		_, _, e = tx.PutOperation(op, before)
		return e
	})
	f.driver.BindRegistrar(f.coordinator)
	if err = f.service.RecoverUnsentResponses(t.Context(), 32); err != nil {
		t.Fatal(err)
	}
	check("答复正在交给 Agent")
	f.tx(func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(id)
		if e != nil {
			return e
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationUnknown
		_, _, e = tx.PutOperation(op, before)
		return e
	})
	check("不会重复发送")
	if f.writes != 1 {
		t.Fatal("status projection resent saved decision")
	}
}
