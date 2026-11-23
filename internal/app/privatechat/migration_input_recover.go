package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// prepareHistoricalInput changes only this in-memory action; the source marker
// and resulting checkpoint are committed together by commitHistoricalRecovery.
func (p *Processor) prepareHistoricalInput(ctx context.Context, o Owner, generation int64, action control.Action) (control.Action, *historicalRecoveryRef, error) {
	parts := strings.Fields(action.Text)
	if action.Kind != control.ActionTextMessage || len(parts) != 3 || parts[0] != "/recover" || parts[1] != "confirm" {
		return action, nil, nil
	}
	n, e := strconv.Atoi(parts[2])
	if e != nil || n < 1 || n > 1000000 {
		action.Text = "/recover invalid"
		return action, nil, nil
	}
	item, found, e := p.journal.HistoricalInputAt(ctx, o, generation, n)
	if e != nil {
		return action, nil, e
	}
	if !found {
		action.Text = "/recover invalid"
		return action, nil, nil
	}
	var recovered int
	e = p.journal.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs WHERE import_id=? AND inbox_id=? AND payload_hash=?`, item.ImportID, historicalMarkerKey(item), item.PayloadHash).Scan(&recovered)
	if e != nil {
		return action, nil, e
	}
	if recovered != 0 {
		action.Text = "/recover already"
		return action, nil, nil
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface == nil || surface.AttachedInstanceID == "" {
		action.Text = "/recover new-session"
		return action, nil, nil
	}
	instance := p.app.Service().Instance(surface.AttachedInstanceID)
	if instance == nil || !instance.Online {
		action.Text = "/recover new-session"
		return action, nil, nil
	}
	newThread := surface.RouteMode == state.RouteModeNewThreadReady && surface.SelectedThreadID == ""
	var proof *historicalSessionProof
	if !newThread {
		proof, e = p.journal.historicalNewSessionProof(ctx, o, generation, surface.AttachedInstanceID, surface.SelectedThreadID, instance.Backend)
		if e != nil {
			return action, nil, e
		}
		if proof == nil {
			action.Text = "/recover new-session"
			return action, nil, nil
		}
	}
	if !item.TextAvailable {
		action.Text = "/recover unavailable"
		return action, nil, nil
	}
	action.RecoveryNewThread = newThread
	if proof != nil {
		action.RecoverySessionID = proof.SessionID
	}
	action.Text = item.Text
	action.Inputs = nil
	action.Files = nil
	if item.Text != "" {
		action.Inputs = append(action.Inputs, agentproto.Input{Type: agentproto.InputText, Text: item.Text})
	}
	for _, a := range item.Attachments {
		if !a.Available {
			action.Text = "/recover unavailable"
			action.Inputs = nil
			action.Files = nil
			return action, nil, nil
		}
		if e = verifyHistoricalAsset(a.Path, a.Attachment.SHA256, a.Attachment.Size); e != nil {
			action.Text = "/recover unavailable"
			action.Inputs = nil
			action.Files = nil
			return action, nil, nil
		}
		if a.Attachment.Kind == "image" {
			action.Inputs = append(action.Inputs, agentproto.Input{Type: agentproto.InputLocalImage, Path: a.Path, MIMEType: a.Attachment.MediaType})
		} else {
			action.Files = append(action.Files, control.ActionFileAttachment{SourceMessageID: action.MessageID, LocalPath: a.Path, FileName: a.Attachment.FileName})
		}
	}
	if len(action.Inputs) == 0 && len(action.Files) == 0 {
		return action, nil, ErrJournalState
	}
	return action, &historicalRecoveryRef{ImportID: item.ImportID, InboxID: item.InboxID, PayloadHash: item.PayloadHash, Kind: item.Kind, Session: proof}, nil
}
func verifyHistoricalAsset(path, digest string, size int64) error {
	if !filepath.IsAbs(path) || size < 0 {
		return ErrJournalConflict
	}
	root, e := os.OpenRoot(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer root.Close()
	info, e := root.Lstat(filepath.Base(path))
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != size {
		return ErrJournalConflict
	}
	f, e := root.Open(filepath.Base(path))
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	if e != nil {
		return e
	}
	if n != size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), digest) {
		return ErrJournalConflict
	}
	return nil
}

func (j *Journal) historicalInputAlreadyRecovered(ctx context.Context, o Owner, generation int64, action control.Action) (bool, error) {
	parts := strings.Fields(action.Text)
	if action.Kind != control.ActionTextMessage || len(parts) != 3 || parts[0] != "/recover" || parts[1] != "confirm" {
		return false, nil
	}
	n, e := strconv.Atoi(parts[2])
	if e != nil || n < 1 || n > 1000000 {
		return false, nil
	}
	item, found, e := j.HistoricalInputAt(ctx, o, generation, n)
	if e != nil || !found {
		return false, e
	}
	var count int
	e = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs WHERE import_id=? AND inbox_id=? AND payload_hash=?`, item.ImportID, historicalMarkerKey(item), item.PayloadHash).Scan(&count)
	return count == 1, e
}

func historicalMarkerKey(item HistoricalInput) string {
	if item.Kind == "queue" {
		return "queue:" + item.InboxID
	}
	return item.InboxID
}
