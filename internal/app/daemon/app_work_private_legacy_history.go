package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workmigration"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) teamClaimedLegacyHistory(ctx context.Context, source teamHumanSource, contextID string) ([]string, error) {
	var items []workstore.MigrationItem
	var result []string
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := a.validateTeamPickerSource(tx, source); e != nil {
			return e
		}
		var e error
		items, e = tx.ListClaimedMigrationHistory(source.ScopeID, contextID)
		return e
	})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		var record workmigration.LegacyRecord
		if err = json.Unmarshal(item.Payload, &record); err != nil {
			return nil, err
		}
		if record.Kind != "surface" {
			continue
		}
		var old surfaceresume.Entry
		if err = json.Unmarshal(record.Raw, &old); err != nil {
			return nil, err
		}
		if old.ResumeThreadID == "" {
			continue
		}
		title := strings.TrimSpace(old.ResumeThreadTitle)
		if title == "" {
			title = "旧工作"
		}
		result = append(result, "已认领的旧记录："+title)
		if old.ResumeThreadCWD != "" {
			result = append(result, "工作目录："+old.ResumeThreadCWD)
		}
		allowed := true
		err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			migration, e := tx.GetMigration(item.MigrationID)
			if e != nil {
				return e
			}
			var snapshot workmigration.Snapshot
			if e = json.Unmarshal(migration.Snapshot, &snapshot); e != nil {
				return e
			}
			for _, other := range snapshot.Records {
				if other.Kind != "surface" {
					continue
				}
				var entry surfaceresume.Entry
				if e = json.Unmarshal(other.Raw, &entry); e != nil {
					return e
				}
				if entry.ResumeThreadID != old.ResumeThreadID || agentproto.NormalizeBackend(agentproto.Backend(entry.Backend)) != agentproto.NormalizeBackend(agentproto.Backend(old.Backend)) {
					continue
				}
				imported, e := tx.GetMigrationItem(item.MigrationID, other.Key)
				if e != nil || imported.State != "imported" {
					allowed = false
					return nil
				}
				unit, e := tx.GetMigrationUnit(item.MigrationID, other.UnitKey)
				if e != nil || unit.PrincipalID != source.PrincipalID || unit.RoleID != source.RoleID {
					allowed = false
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !allowed {
			result = append(result, "这份原生历史还关联其他未核对归属，暂不展开内容。")
			continue
		}
		catalog, e := newDaemonPersistedThreadCatalog(nil)
		if e != nil {
			result = append(result, "旧会话内容暂时无法读取。")
			continue
		}
		thread, e := catalog.ThreadByIDForBackend(agentproto.Backend(old.Backend), old.ResumeThreadID)
		if e != nil || thread == nil {
			result = append(result, "旧会话内容暂时无法读取。")
			continue
		}
		for _, part := range []struct{ label, text string }{{"最初需求", thread.FirstUserMessage}, {"最近需求", thread.LastUserMessage}, {"最近回复", thread.LastAssistantMessage}} {
			if text := strings.TrimSpace(part.text); text != "" {
				result = append(result, fmt.Sprintf("%s：\n%s", part.label, clipTeamHistory(text, 5000)))
			}
		}
		if len(result) >= 16 {
			break
		}
	}
	return result, nil
}
func clipTeamHistory(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "\n…"
	}
	return text
}
