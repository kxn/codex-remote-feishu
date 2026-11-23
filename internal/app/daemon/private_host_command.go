package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type privateMkdirReceipt struct {
	Version                             int
	EffectID, SurfaceID, PickerID, Path string
	Succeeded                           bool
}
type privateMkdirIntent struct {
	Version                      int
	Parent, Name, ParentIdentity string
}
type privateLocalVerifier struct {
	owner      privatechat.Owner
	generation int64
	effect     privatechat.Effect
	receipt    json.RawMessage
}

func (v privateLocalVerifier) VerifyEffect(_ context.Context, o privatechat.Owner, g int64, e privatechat.Effect, r json.RawMessage) error {
	if o != v.owner || g != v.generation || e.ID != v.effect.ID || string(e.Body) != string(v.effect.Body) || string(r) != string(v.receipt) {
		return privatechat.ErrJournalConflict
	}
	return nil
}

// runPrivateMkdir runs only the committed exact command. Recovery rechecks the
// durable parent identity and the actual directory before idempotent creation.
func runPrivateMkdir(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, id string) ([]privatechat.EffectRecord, error) {
	record, err := j.LoadEffect(ctx, o, generation, id)
	if err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(record.Effect.Body)
	if err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateMkdir || event.SurfaceSessionID != o.SurfaceID || cmd.SurfaceSessionID != o.SurfaceID || cmd.PickerID == "" || !filepath.IsAbs(cmd.LocalPath) || filepath.Clean(cmd.LocalPath) != cmd.LocalPath {
		return nil, privatechat.ErrJournalConflict
	}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateMkdirReducer{host})
	}
	if record.State != "prepared" && record.State != "unknown" {
		return nil, fmt.Errorf("private directory result requires reconciliation")
	}
	parent, name := filepath.Dir(cmd.LocalPath), filepath.Base(cmd.LocalPath)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || canonical != parent {
		return nil, fmt.Errorf("private directory parent changed")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	parentFile, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer parentFile.Close()
	identity, err := privateDirectoryIdentity(parentFile)
	if err != nil {
		return nil, err
	}
	intent := privateMkdirIntent{Version: 1, Parent: parent, Name: name, ParentIdentity: identity}
	intentRaw, err := json.Marshal(intent)
	if err != nil {
		return nil, err
	}
	stored, err := j.LoadLocalIntent(ctx, o, generation, id)
	if errors.Is(err, sql.ErrNoRows) && record.State == "prepared" {
		if _, statErr := root.Lstat(name); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("private directory target already exists or is inaccessible")
		}
		if err = j.PrepareLocalIntent(ctx, o, generation, id, intentRaw); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if string(stored) != string(intentRaw) {
		return nil, fmt.Errorf("private directory parent identity changed")
	}
	if record.State == "prepared" {
		if err = j.MarkEffectSending(ctx, o, generation, id); err != nil {
			return nil, err
		}
	}
	result := privateMkdirReceipt{Version: 1, EffectID: id, SurfaceID: o.SurfaceID, PickerID: cmd.PickerID, Path: cmd.LocalPath}
	if info, statErr := root.Lstat(name); errors.Is(statErr, os.ErrNotExist) {
		err = root.Mkdir(name, 0755)
		if errors.Is(err, os.ErrExist) {
			err = nil
		}
	} else if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("private directory target changed")
	}
	if err == nil {
		info, e := root.Lstat(name)
		if e != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("private directory result invalid")
		}
		child, e := root.Open(name)
		if e != nil {
			return nil, e
		}
		opened, e := child.Stat()
		child.Close()
		if e != nil || !opened.IsDir() || !os.SameFile(info, opened) {
			return nil, fmt.Errorf("private directory identity changed")
		}
		current, e := os.Open(parent)
		if e != nil {
			return nil, e
		}
		currentIdentity, e := privateDirectoryIdentity(current)
		current.Close()
		if e != nil || currentIdentity != identity {
			return nil, fmt.Errorf("private directory parent replaced")
		}
	}
	result.Succeeded = err == nil
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	verifier := privateLocalVerifier{o, generation, record.Effect, raw}
	if err = j.ConfirmLocalEffect(context.WithoutCancel(ctx), o, generation, id, raw, verifier); err != nil {
		return nil, err
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, id, privateMkdirReducer{host})
}

type privateMkdirReducer struct{ host *App }

func (r privateMkdirReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	event, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privateMkdirReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	cmd := event.DaemonCommand
	if cmd == nil || cmd.Kind != control.DaemonCommandPrivateMkdir || receipt.Version != 1 || receipt.EffectID != effect.ID || receipt.SurfaceID != event.SurfaceSessionID || receipt.SurfaceID != cmd.SurfaceSessionID || receipt.PickerID != cmd.PickerID || receipt.Path != cmd.LocalPath {
		return nil, privatechat.ErrJournalConflict
	}
	return app.Service().CompletePrivateLocalDirectory(receipt.SurfaceID, receipt.PickerID, receipt.Path, receipt.Succeeded)
}
