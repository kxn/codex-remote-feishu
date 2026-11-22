package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

func (t *Tx) ListSystemHostJobs() ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", ` WHERE id IN (SELECT o.id FROM operations o WHERE kind='host.admin.weblink' AND NOT EXISTS (SELECT 1 FROM json_each(o.payload,'$.steps') s WHERE json_extract(s.value,'$.id')='ui_result') ORDER BY json_extract(o.payload,'$.updated_at'),o.id LIMIT 16)`)
}
