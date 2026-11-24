package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestRetiredThreadEvidenceNotLimitedByLaterEvents(t *testing.T) {
	j, p, lease, _, binding := effectFixture(t)
	ref := persistNative(t, t.Context(), lease, binding, 1, agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", CWD: "/tmp/workspace"})
	if _, err := p.ApplyNative(t.Context(), lease, ref, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	_, err := j.db.Exec(`WITH RECURSIVE n(v) AS (SELECT 2 UNION ALL SELECT v+1 FROM n WHERE v<1202) INSERT INTO native_events(incarnation,sequence,payload_hash,command_id,payload,applied,applied_revision) SELECT 'inc',v,'unrelated','', '{"kind":"turn.completed","threadId":"other"}',1,1 FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	event, err := p.appliedRetiredThreadFact(t.Context(), lease.native, "thread")
	if err != nil || event == nil || event.CWD != "/tmp/workspace" {
		t.Fatalf("old thread evidence=%v err=%v", event != nil, err)
	}
}
