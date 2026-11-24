package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const nativeConnectionSchema = `CREATE TABLE IF NOT EXISTS private_native_connections (incarnation TEXT NOT NULL, connection_id TEXT NOT NULL, revision INTEGER NOT NULL, PRIMARY KEY(incarnation,connection_id)); CREATE TABLE IF NOT EXISTS private_native_connection_hellos (incarnation TEXT NOT NULL, connection_id TEXT NOT NULL, hello_hash TEXT NOT NULL, PRIMARY KEY(incarnation,connection_id));`

type nativeConnectionRef struct{ Incarnation, ConnectionID, HelloHash string }

func (j *Journal) commitNativeConnection(ctx context.Context, tx *sql.Tx, cp Checkpoint, ref nativeConnectionRef) error {
	if ref.Incarnation == "" || ref.ConnectionID == "" {
		return ErrJournalConflict
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO private_native_connections(incarnation,connection_id,revision) VALUES(?,?,?)`, ref.Incarnation, ref.ConnectionID, cp.Revision)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO private_native_connection_hellos(incarnation,connection_id,hello_hash) VALUES(?,?,?)`, ref.Incarnation, ref.ConnectionID, ref.HelloHash)
	return err
}

// ApplyNativeConnected projects one authenticated socket hello into the core.
// A replay of the same socket returns the original effects; a reconnect has a
// new identity and may update connectivity without consuming an input twice.
func (p *Processor) ApplyNativeConnected(ctx context.Context, l *NativeLease, connectionID string) ([]EffectRecord, error) {
	if l == nil || l.journal != p.journal || connectionID == "" {
		return nil, ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	var out []EffectRecord
	err := l.with(ctx, func() error {
		var err error
		out, err = p.applyNativeConnectedLocked(ctx, l, connectionID, "")
		return err
	})
	return out, err
}

// ApplyAuthenticatedNativeHello registers and projects a trusted connection
// under one owner lease and processor lock; no partially updated online state
// can leak if the checkpoint commit fails.
func (p *Processor) ApplyAuthenticatedNativeHello(ctx context.Context, l *NativeLease, hello agentproto.Hello, connectionID string) ([]EffectRecord, error) {
	if l == nil || l.journal != p.journal || connectionID == "" || hello.Probe || hello.PrivateNativeProtocolVersion != agentproto.PrivateNativeProtocolVersion || hello.CarrierIncarnation != l.native.CarrierIncarnation || hello.Instance.InstanceID != l.native.InstanceID || agentproto.EffectiveHelloBackend(hello) != l.native.Backend {
		return nil, ErrJournalConflict
	}
	unlock := p.journal.LockReferenceMutation()
	defer unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned {
		return nil, ErrProcessorPoisoned
	}
	raw, err := json.Marshal(hello)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	helloHash := hex.EncodeToString(digest[:])
	var out []EffectRecord
	err = l.with(ctx, func() error {
		if err := p.registerNativeHelloLocked(ctx, l, hello); err != nil {
			return err
		}
		var err error
		out, err = p.applyNativeConnectedLocked(ctx, l, connectionID, helloHash)
		if err != nil {
			p.poisoned = true
		}
		return err
	})
	return out, err
}

func (p *Processor) applyNativeConnectedLocked(ctx context.Context, l *NativeLease, connectionID, helloHash string) ([]EffectRecord, error) {
	var revision int64
	var storedHash string
	err := p.journal.db.QueryRowContext(ctx, `SELECT c.revision,h.hello_hash FROM private_native_connections c JOIN private_native_connection_hellos h ON h.incarnation=c.incarnation AND h.connection_id=c.connection_id WHERE c.incarnation=? AND c.connection_id=?`, l.native.CarrierIncarnation, connectionID).Scan(&revision, &storedHash)
	if err == nil {
		if storedHash != helloHash {
			return nil, ErrJournalConflict
		}
		return p.journal.processorEffects(ctx, l.surface, l.generation, revision)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	owner, err := p.ensureOwner(ctx, l.surface, l.generation)
	if err != nil {
		return nil, err
	}
	if p.app.instances[l.native.InstanceID] != l.surface.SurfaceID {
		return nil, ErrJournalConflict
	}
	instance := p.app.Service().Instance(l.native.InstanceID)
	if instance == nil || !instance.Online {
		return nil, ErrJournalConflict
	}
	events := p.app.Service().ApplyInstanceConnected(l.native.InstanceID)
	effects, err := p.freezeEffects(l.surface, l.generation, "native-connect:"+l.native.CarrierIncarnation+":"+connectionID, events)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(l.surface.SurfaceID)
	if err != nil {
		p.poisoned = true
		return nil, err
	}
	cp := Checkpoint{Owner: l.surface, Generation: l.generation, Revision: owner.revision + 1, Cursor: owner.cursor, Core: core}
	ref := migrationResumeRef{Connection: &nativeConnectionRef{Incarnation: l.native.CarrierIncarnation, ConnectionID: connectionID, HelloHash: helloHash}}
	if err = p.journal.commitCheckpointExtended(ctx, cp, owner.revision, effects, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err != nil {
		p.poisoned = true
		return nil, err
	}
	p.owners[l.surface] = processorOwner{cp.Revision, cp.Cursor, l.generation}
	return p.journal.processorEffects(ctx, l.surface, l.generation, cp.Revision)
}
