package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type historicalSessionProof struct {
	Incarnation, Hash, CommandHash, SessionID string
	Sequence                                  uint64
}

// historicalNewSessionProof accepts only a durable native acceptance of a
// target-created thread and matches it to the current authenticated carrier.
func (j *Journal) historicalNewSessionProof(ctx context.Context, o Owner, generation int64, instanceID, sessionID string, backend agentproto.Backend) (*historicalSessionProof, error) {
	if instanceID == "" || sessionID == "" {
		return nil, nil
	}
	rows, err := j.db.QueryContext(ctx, `SELECT owner_json,surface_json FROM native_carriers WHERE generation=? AND json_extract(owner_json,'$.instanceId')=?`, generation, instanceID)
	if err != nil {
		return nil, err
	}
	var current agentproto.NativeOwner
	found := false
	for rows.Next() {
		var raw, surfaceRaw []byte
		if err = rows.Scan(&raw, &surfaceRaw); err != nil {
			rows.Close()
			return nil, err
		}
		var n agentproto.NativeOwner
		var s Owner
		if json.Unmarshal(raw, &n) != nil || json.Unmarshal(surfaceRaw, &s) != nil {
			rows.Close()
			return nil, ErrJournalConflict
		}
		if s != o {
			continue
		}
		if found {
			rows.Close()
			return nil, ErrJournalConflict
		}
		current = n
		found = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if !found || current.Validate() != nil || current.Backend != backend {
		return nil, nil
	}
	rows, err = j.db.QueryContext(ctx, `SELECT e.incarnation,e.sequence,e.payload,e.payload_hash,c.payload,c.payload_hash,car.owner_json,car.surface_json,d.namespace,d.backend FROM native_events e JOIN native_commands c ON c.command_id=e.command_id JOIN dispatch d ON d.id=c.dispatch_id JOIN native_carriers car ON car.incarnation=e.incarnation WHERE d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.state='accepted' AND d.event_sequence=e.sequence AND d.carrier=e.incarnation AND json_extract(e.payload,'$.threadId')=? ORDER BY e.sequence DESC LIMIT 100`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var proof historicalSessionProof
		var eventRaw, commandRaw, ownerRaw, surfaceRaw []byte
		var namespace, backendText string
		if err = rows.Scan(&proof.Incarnation, &proof.Sequence, &eventRaw, &proof.Hash, &commandRaw, &proof.CommandHash, &ownerRaw, &surfaceRaw, &namespace, &backendText); err != nil {
			return nil, err
		}
		var ev agentproto.Event
		var cmd agentproto.Command
		var native agentproto.NativeOwner
		var surface Owner
		if json.Unmarshal(eventRaw, &ev) != nil || json.Unmarshal(commandRaw, &cmd) != nil || json.Unmarshal(ownerRaw, &native) != nil || json.Unmarshal(surfaceRaw, &surface) != nil {
			return nil, ErrJournalConflict
		}
		sum := sha256.Sum256(commandRaw)
		hash, e := agentproto.RuntimeEventPayloadHash(ev)
		if e != nil || hash != proof.Hash || hex.EncodeToString(sum[:]) != proof.CommandHash {
			return nil, ErrJournalConflict
		}
		if surface != o || native.Validate() != nil || ev.NativeBinding == nil || cmd.NativeBinding == nil || *ev.NativeBinding != *cmd.NativeBinding || ev.NativeBinding.Owner != native || ev.CommandID != cmd.CommandID || ev.ThreadID != sessionID || (ev.Kind != agentproto.EventNativeTurnBindingObserved && ev.Kind != agentproto.EventTurnStarted) || cmd.Kind != agentproto.CommandPromptSend || agentproto.NormalizePromptExecutionMode(cmd.Target.ExecutionMode) != agentproto.PromptExecutionModeStartNew || !cmd.Target.CreateThreadIfMissing || cmd.Target.ThreadID != "" || namespace != native.HistoryNamespace || backendText != string(native.Backend) {
			continue
		}
		if native.OwnerID != current.OwnerID || native.Epoch != current.Epoch || native.Application != current.Application || native.Backend != current.Backend || native.HistoryNamespace != current.HistoryNamespace {
			continue
		}
		proof.SessionID = sessionID
		return &proof, nil
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}
