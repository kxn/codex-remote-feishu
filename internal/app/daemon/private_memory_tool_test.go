package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type memoryImportFixture struct {
	bundle privatechat.MigrationImportBundle
}

func (f memoryImportFixture) LoadVerifiedMigration(context.Context, privatechat.MigrationImportRequest) (privatechat.MigrationImportBundle, error) {
	return f.bundle, nil
}
func memoryFixtureHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func memoryFixtureRecord(table, key, scope string, payload, data []byte, revision int64) privatechat.MigrationImportedRecord {
	return privatechat.MigrationImportedRecord{Table: table, Key: key, ScopeID: scope, Revision: revision, Payload: payload, PayloadHash: memoryFixtureHash(payload), Data: data, DataHash: memoryFixtureHash(data)}
}

func TestPrivateMemoryToolUsesAuthenticatedOwnerAndRevision(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	journal, err := privatechat.OpenJournal(filepath.Join(dir, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	checkpoint := orchestrator.PrivateCheckpoint{Version: 1, SurfaceID: owner.SurfaceID, GatewayID: owner.GatewayID, ChatID: owner.ChatID, ActorUserID: owner.UserID, RouteMode: state.RouteModeUnbound, PickerWatermarks: &orchestrator.PrivatePickerWatermarks{}, Holds: []orchestrator.PrivateRecoveryHold{{Kind: "migration_source_unverified", Frozen: json.RawMessage(`{}`)}}}
	scope := memoryFixtureRecord("scopes", "scope", "scope", []byte(`{"id":"scope","kind":"personal"}`), nil, 1)
	entry := memoryFixtureRecord("memories", "memory", "scope", []byte(`{"id":"memory","storage_scope_id":"scope","scope":{"kind":"personal"},"revision":2,"state":"valid","content_ref":"body","applicability":"user preferences","sources":[{"id":"source"}]}`), nil, 2)
	body := memoryFixtureRecord("content", "body", "scope", []byte(`{"scope_id":"scope"}`), []byte("prefers concise replies"), 0)
	manifestBytes := []byte(`{"source":{"scope_id":"scope"}}`)
	manifestHash := memoryFixtureHash(manifestBytes)
	manifest := memoryFixtureRecord("migration_manifest", manifestHash, "scope", manifestBytes, nil, 0)
	loader := memoryImportFixture{privatechat.MigrationImportBundle{SourceScope: "scope", SourceHash: manifestHash, ProofHash: memoryFixtureHash([]byte("proof")), Checkpoint: checkpoint, Records: []privatechat.MigrationImportedRecord{scope, entry, body, manifest}}}
	importer, err := privatechat.NewMigrationImporter(journal, loader, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = importer.Import(ctx, privatechat.MigrationImportRequest{ImportID: "i", SourceHandle: "sealed", Owner: owner, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	h := &privateNativeHost{journal: journal}
	caller := &privateToolCaller{runtime: &privateToolRuntime{host: h}, record: privateCarrierRecord{Surface: owner, SurfaceGeneration: 1}}
	app := &App{}
	if _, e := app.privateMemoryTool(ctx, map[string]any{"action": "list"}); e == nil {
		t.Fatal("unauthenticated memory access")
	}
	bound := context.WithValue(ctx, privateToolCallerKey{}, caller)
	result, e := app.privateMemoryTool(bound, map[string]any{"action": "read", "id": "memory", "revision": float64(2)})
	if e != nil || result.(privatechat.Memory).Content != "prefers concise replies" {
		t.Fatal("memory content unavailable")
	}
	if _, e = app.privateMemoryTool(bound, map[string]any{"action": "read", "id": "memory", "revision": float64(1)}); e == nil {
		t.Fatal("stale memory revision accepted")
	}
	write := map[string]any{"action": "write", "id": "memory", "operation_id": "correct-preference", "expected_revision": float64(2), "content": "prefers detailed answers for design reviews", "applicability": "design reviews"}
	if _, e = app.privateMemoryTool(bound, write); e != nil {
		t.Fatal(e)
	}
	result, e = app.privateMemoryTool(bound, map[string]any{"action": "list"})
	if e != nil {
		t.Fatal(e)
	}
	index := result.(map[string]any)["memories"].([]privatechat.Memory)
	if len(index) != 1 || index[0].Revision != 3 || index[0].Content != "" {
		t.Fatalf("import not replaced by metadata-only index: %+v", index)
	}
	if _, e = app.privateMemoryTool(bound, map[string]any{"action": "read", "id": "memory", "revision": float64(2)}); e == nil {
		t.Fatal("old imported version bypassed correction")
	}
	if _, e = app.privateMemoryTool(bound, map[string]any{"action": "revoke", "id": "memory", "operation_id": "forget-preference", "expected_revision": float64(3)}); e != nil {
		t.Fatal(e)
	}
	// Retrying the earlier write acknowledges it but must not republish its
	// body or restore the revoked imported record.
	result, e = app.privateMemoryTool(bound, write)
	if e != nil || result.(map[string]any)["content"] != nil {
		t.Fatalf("replay exposed stale body: %+v %v", result, e)
	}
	result, e = app.privateMemoryTool(bound, map[string]any{"action": "list"})
	if e != nil || len(result.(map[string]any)["memories"].([]privatechat.Memory)) != 0 {
		t.Fatal("revoked import appeared again")
	}
	caller.record.Surface.UserID = "other"
	if _, e = app.privateMemoryTool(bound, map[string]any{"action": "read", "id": "memory", "revision": float64(2)}); e == nil {
		t.Fatal("cross-owner memory access")
	}
}
