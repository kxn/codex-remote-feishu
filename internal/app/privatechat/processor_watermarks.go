package privatechat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// privateWatermarks reads only numeric watermarks from each committed owner
// checkpoint. It does not materialize another owner's surfaces or inputs.
func (j *Journal) privateWatermarks(ctx context.Context) (orchestrator.PrivateIDWatermarks, error) {
	var maximum orchestrator.PrivateIDWatermarks
	rows, err := j.db.QueryContext(ctx, `SELECT data FROM private_checkpoint`)
	if err != nil {
		return maximum, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return maximum, err
		}
		var c struct {
			Version                                                                                         int
			NextQueueID, NextImageID, NextFileID, NextCommandID, NextHeadlessID, NextEpisodeID, NextShellID int
			PickerWatermarks                                                                                *orchestrator.PrivatePickerWatermarks
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return maximum, fmt.Errorf("decode private checkpoint watermark: %w", err)
		}
		w, e := (orchestrator.PrivateCheckpoint{Version: c.Version, NextQueueID: c.NextQueueID, NextImageID: c.NextImageID, NextFileID: c.NextFileID, NextCommandID: c.NextCommandID, NextHeadlessID: c.NextHeadlessID, NextEpisodeID: c.NextEpisodeID, NextShellID: c.NextShellID, PickerWatermarks: c.PickerWatermarks}).IDWatermarks()
		if e != nil {
			return maximum, e
		}
		maximum = mergePrivateWatermarks(maximum, w)
	}
	return maximum, rows.Err()
}
func mergePrivateWatermarks(a, b orchestrator.PrivateIDWatermarks) orchestrator.PrivateIDWatermarks {
	a.Queue = max(a.Queue, b.Queue)
	a.Image = max(a.Image, b.Image)
	a.File = max(a.File, b.File)
	a.Request = max(a.Request, b.Request)
	a.Headless = max(a.Headless, b.Headless)
	a.Episode = max(a.Episode, b.Episode)
	a.Shell = max(a.Shell, b.Shell)
	a.Pickers.Path = max(a.Pickers.Path, b.Pickers.Path)
	a.Pickers.Target = max(a.Pickers.Target, b.Pickers.Target)
	a.Pickers.History = max(a.Pickers.History, b.Pickers.History)
	a.Pickers.Review = max(a.Pickers.Review, b.Pickers.Review)
	a.Pickers.Compact = max(a.Pickers.Compact, b.Pickers.Compact)
	a.Pickers.Plan = max(a.Pickers.Plan, b.Pickers.Plan)
	a.Pickers.Launcher = max(a.Pickers.Launcher, b.Pickers.Launcher)
	return a
}
