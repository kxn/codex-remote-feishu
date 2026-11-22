package workstore

import "encoding/json"

func (t *Tx) ListWaitingModelCatalogViews(after string, limit int) ([]UIView, error) {
	if limit < 1 || limit > 100 {
		limit = 32
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM team_ui_views WHERE id>? AND json_extract(payload,'$.kind')='bot.settings' AND coalesce(json_extract(payload,'$.invalidated'),0)=0 AND EXISTS(SELECT 1 FROM content c WHERE c.id=team_ui_views.id||':catalog-wait') AND NOT EXISTS(SELECT 1 FROM content c WHERE c.id=team_ui_views.id||':catalog-result') ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []UIView
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var value UIView
		if err = json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
