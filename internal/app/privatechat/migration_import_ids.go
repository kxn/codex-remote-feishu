package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func migrationWatermarks(ctx context.Context, tx *sql.Tx) (orchestrator.PrivateIDWatermarks, error) {
	var max orchestrator.PrivateIDWatermarks
	rows, err := tx.QueryContext(ctx, `SELECT data FROM private_checkpoint`)
	if err != nil {
		return max, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var c orchestrator.PrivateCheckpoint
		if err = rows.Scan(&raw); err != nil {
			return max, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return max, err
		}
		w, e := c.IDWatermarks()
		if e != nil {
			return max, e
		}
		max = mergePrivateWatermarks(max, w)
	}
	return max, rows.Err()
}
func migrationID(id, prefix string, limit, base int) (string, error) {
	if !strings.HasPrefix(id, prefix) {
		return "", ErrJournalState
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, prefix))
	if err != nil || n < 1 || n > limit || n > int(^uint(0)>>1)-base {
		return "", ErrJournalState
	}
	return fmt.Sprintf("%s%d", prefix, base+n), nil
}
func remapMigrationCheckpoint(input orchestrator.PrivateCheckpoint, base orchestrator.PrivateIDWatermarks) (orchestrator.PrivateCheckpoint, error) {
	var c orchestrator.PrivateCheckpoint
	raw, err := json.Marshal(input)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if _, err = c.IDWatermarks(); err != nil {
		return c, err
	}
	queue := map[string]*state.QueueItemRecord{}
	queueIDs := map[string]string{}
	for old, item := range c.QueueItems {
		if item == nil || item.ID != old {
			return c, ErrJournalState
		}
		id, e := migrationID(old, "queue-", c.NextQueueID, base.Queue)
		if e != nil {
			return c, e
		}
		queueIDs[old] = id
		item.ID = id
		queue[id] = item
	}
	c.QueueItems = queue
	for n, id := range c.QueuedQueueItemIDs {
		mapped, ok := queueIDs[id]
		if !ok {
			return c, ErrJournalState
		}
		c.QueuedQueueItemIDs[n] = mapped
	}
	if c.ActiveQueueItemID != "" {
		mapped, ok := queueIDs[c.ActiveQueueItemID]
		if !ok {
			return c, ErrJournalState
		}
		c.ActiveQueueItemID = mapped
	}
	for n := range c.Holds {
		if c.Holds[n].QueueItemID != "" {
			mapped, ok := queueIDs[c.Holds[n].QueueItemID]
			if !ok {
				return c, ErrJournalState
			}
			c.Holds[n].QueueItemID = mapped
		}
	}
	images := map[string]*state.StagedImageRecord{}
	for old, image := range c.StagedImages {
		if image == nil || image.ImageID != old {
			return c, ErrJournalState
		}
		id, e := migrationID(old, "img-", c.NextImageID, base.Image)
		if e != nil {
			return c, e
		}
		image.ImageID = id
		images[id] = image
	}
	c.StagedImages = images
	files := map[string]*state.StagedFileRecord{}
	for old, file := range c.StagedFiles {
		if file == nil || file.FileID != old {
			return c, ErrJournalState
		}
		id, e := migrationID(old, "file-", c.NextFileID, base.File)
		if e != nil {
			return c, e
		}
		file.FileID = id
		files[id] = file
	}
	c.StagedFiles = files
	if c.NextQueueID > int(^uint(0)>>1)-base.Queue || c.NextImageID > int(^uint(0)>>1)-base.Image || c.NextFileID > int(^uint(0)>>1)-base.File {
		return c, ErrJournalState
	}
	c.NextQueueID += base.Queue
	c.NextImageID += base.Image
	c.NextFileID += base.File
	w, e := c.IDWatermarks()
	if e != nil {
		return c, e
	}
	w = mergePrivateWatermarks(w, base)
	c.NextCommandID = w.Request
	c.NextHeadlessID = w.Headless
	c.NextEpisodeID = w.Episode
	c.NextShellID = w.Shell
	c.PickerWatermarks = &w.Pickers
	return c, nil
}
