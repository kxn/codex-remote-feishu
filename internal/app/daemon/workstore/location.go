package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

func (t *Tx) PutLocation(v work.ExecutionLocation, expected int64) error {
	if expected > 0 {
		old, err := t.GetLocation(v.ID)
		if err != nil {
			return err
		}
		if v.Generation < old.Generation || v.Generation > old.Generation+1 {
			return fmt.Errorf("invalid location generation")
		}
		if (old.HostID != v.HostID || old.Cwd != v.Cwd || !reflect.DeepEqual(old.ResourceKeys, v.ResourceKeys)) && v.Generation != old.Generation+1 {
			return fmt.Errorf("location change requires generation")
		}
		if v.Generation != old.Generation {
			var count int
			if err = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM resource_occupancy o JOIN attempts a ON a.id=o.attempt_id JOIN workers w ON w.id=a.worker_id WHERE json_extract(w.payload,'$.location_id')=?`, v.ID).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("occupied location cannot change")
			}
		}
	}
	return t.record("locations", v.ID, v.Revision, expected, v)
}
func (t *Tx) GetLocation(id string) (work.ExecutionLocation, error) {
	var v work.ExecutionLocation
	err := t.read("locations", id, &v)
	return v, err
}
func (t *Tx) ListLocations() ([]work.ExecutionLocation, error) {
	return listRecords[work.ExecutionLocation](t, "locations", "")
}
