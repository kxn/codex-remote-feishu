package daemon

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func mapPersonalMigrationInputs(report *PrivateMigrationReport, c *orchestrator.PrivateCheckpoint, rows map[string][]workstore.PersonalExportRecord, content map[string]workstore.PersonalExportRecord, nav work.PrivateNavigation, owner privatechat.Owner, assets []PersonalMigrationAsset) error {
	messages := map[string]work.Message{}
	for _, r := range rows["messages"] {
		var m work.Message
		if err := json.Unmarshal(r.Payload, &m); err != nil {
			return err
		}
		messages[m.ID] = m
	}
	type pending struct {
		inbox work.Inbox
		order int64
	}
	var inputs []pending
	for _, r := range rows["inbox"] {
		var in work.Inbox
		if err := json.Unmarshal(r.Payload, &in); err != nil {
			return err
		}
		if in.State == work.InboxPending || in.State == work.InboxClaimed {
			inputs = append(inputs, pending{in, r.SourceOrder})
		}
	}
	sort.SliceStable(inputs, func(i, j int) bool {
		a, b := inputs[i], inputs[j]
		if !a.inbox.AvailableAt.Equal(b.inbox.AvailableAt) {
			return a.inbox.AvailableAt.Before(b.inbox.AvailableAt)
		}
		return a.order < b.order
	})
	for _, v := range inputs {
		in := v.inbox
		block := func(reason string) {
			report.Blockers = append(report.Blockers, PrivateMigrationBlocker{"inbox", in.ID, reason})
			raw, _ := json.Marshal(in)
			report.InputHolds = append(report.InputHolds, PrivateMigrationInputHold{InboxID: in.ID, MessageID: in.MessageID, Reason: reason, PayloadHash: migrationBytesHash(raw)})
		}
		if in.State == work.InboxClaimed {
			block("claimed_input_requires_native_reconciliation")
			continue
		}
		if in.ContentState == work.InputContentPending || in.ContentState == work.InputContentPartial {
			block("input_content_not_complete")
			continue
		}
		if nav.Mode != work.PrivatePersonal || in.RecipientContextID != nav.FrontContextID || in.TaskID != "" || in.RelatedTaskID != "" || in.ShellOperationID != "" || in.ControlOfInputID != "" || in.AutomationEpisodeID != "" || in.SessionGoalIntentID != "" {
			block("input_route_requires_reconciliation")
			continue
		}
		m, ok := messages[in.MessageID]
		if !ok || m.Source.Kind != work.ActorPrincipal || m.Source.ID != sourcePrincipal(report, rows) || m.PlatformKey == nil || m.PlatformKey.Tenant != owner.TenantID || m.PlatformKey.ChatID != owner.ChatID {
			block("input_platform_source_unverified")
			continue
		}
		images := []PersonalMigrationAsset{}
		for _, a := range m.Attachments {
			found := false
			for _, asset := range assets {
				if asset.ContentID == a.ContentRef && asset.SourceMessageID == a.SourceMessageID && asset.SHA256 == a.SHA256 && asset.Size == a.Size && asset.MediaType == a.MediaType && strings.HasPrefix(a.MediaType, "image/") {
					images = append(images, asset)
					found = true
					break
				}
			}
			if !found {
				images = nil
				break
			}
		}
		if len(images) != len(m.Attachments) || (in.AwaitingText && len(images) == 0) {
			block("media_requires_target_managed_file_import")
			continue
		}
		body, ok := content[m.BodyRef]
		if !ok || !utf8.Valid(body.Data) {
			block("text_content_unavailable")
			continue
		}
		var metadata struct {
			MediaType string `json:"media_type"`
		}
		if json.Unmarshal(body.Payload, &metadata) != nil || !strings.HasPrefix(metadata.MediaType, "text/plain") {
			block("input_content_type_not_proven_text")
			continue
		}
		text := string(body.Data)
		if in.AwaitingText {
			for _, image := range images {
				c.NextImageID++
				id := fmt.Sprintf("img-%d", c.NextImageID)
				c.StagedImages[id] = &state.StagedImageRecord{ImageID: id, SurfaceSessionID: owner.SurfaceID, SourceMessageID: image.SourceMessageID, ActorUserID: owner.UserID, LocalPath: image.Path, MIMEType: image.MediaType, State: state.ImageStaged}
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(text), "/") {
			block("command_requires_target_ingress_replay")
			continue
		}
		c.NextQueueID++
		id := fmt.Sprintf("queue-%d", c.NextQueueID)
		c.QueueItems[id] = &state.QueueItemRecord{ID: id, SurfaceSessionID: owner.SurfaceID, ActorUserID: owner.UserID, SourceKind: state.QueueItemSourceUser, SourceMessageID: m.PlatformKey.MessageID, SourceMessageIDs: []string{m.PlatformKey.MessageID}, Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: text}}, Status: state.QueueItemQueued, RouteModeAtEnqueue: state.RouteModeUnbound}
		for _, image := range images {
			c.NextImageID++
			imageID := fmt.Sprintf("img-%d", c.NextImageID)
			c.StagedImages[imageID] = &state.StagedImageRecord{ImageID: imageID, SurfaceSessionID: owner.SurfaceID, SourceMessageID: image.SourceMessageID, ActorUserID: owner.UserID, LocalPath: image.Path, MIMEType: image.MediaType, State: state.ImageBound}
			foundSource := false
			for _, sourceID := range c.QueueItems[id].SourceMessageIDs {
				if sourceID == image.SourceMessageID {
					foundSource = true
					break
				}
			}
			if !foundSource {
				c.QueueItems[id].SourceMessageIDs = append(c.QueueItems[id].SourceMessageIDs, image.SourceMessageID)
			}
			c.QueueItems[id].Inputs = append(c.QueueItems[id].Inputs, agentproto.Input{Type: agentproto.InputLocalImage, Path: image.Path, MIMEType: image.MediaType})
		}
		c.QueuedQueueItemIDs = append(c.QueuedQueueItemIDs, id)
	}
	return nil
}
func sourcePrincipal(_ *PrivateMigrationReport, rows map[string][]workstore.PersonalExportRecord) string {
	if len(rows["scopes"]) != 1 {
		return ""
	}
	var scope work.WorkScope
	if json.Unmarshal(rows["scopes"][0].Payload, &scope) != nil {
		return ""
	}
	return scope.PrincipalID
}
