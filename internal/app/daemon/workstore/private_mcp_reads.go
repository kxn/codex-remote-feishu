package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// Bound each scheduler pass; completed UI receipts stop being scanned.
func (t *Tx) ListPrivateMCPReads() ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", ` WHERE id IN (SELECT id FROM operations o WHERE kind='native.user.mcp.read' AND NOT EXISTS (SELECT 1 FROM json_each(o.payload,'$.steps') s WHERE json_extract(s.value,'$.id')='ui_result') ORDER BY json_extract(o.payload,'$.updated_at'),id LIMIT 32)`)
}
