package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// nativeReadProjection identifies only an exact, durably settled read result.
// The caller must still recheck current session ownership under the registry fence.
func (j *Journal) nativeReadProjection(ctx context.Context, l *NativeLease, ref NativeEventRef) (session, operation string, expired bool, err error) {
	var raw, commandRaw []byte
	var hash, commandHash, dispatch, status string
	var sequence int64
	err = j.db.QueryRowContext(ctx, `SELECT e.payload,e.payload_hash,n.payload,n.payload_hash,d.id,d.state,d.event_sequence FROM native_events e JOIN native_commands n ON n.command_id=e.command_id JOIN dispatch d ON d.id=n.dispatch_id WHERE e.incarnation=? AND e.sequence=? AND d.tenant=? AND d.app_id=? AND d.gateway=? AND d.surface=? AND d.chat=? AND d.user_id=? AND d.generation=? AND d.carrier=? AND d.state IN ('accepted','failed') AND d.event_sequence=e.sequence`, ref.Incarnation, ref.Sequence, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation, ref.Incarnation).Scan(&raw, &hash, &commandRaw, &commandHash, &dispatch, &status, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	var event agentproto.Event
	var command agentproto.Command
	if hash != ref.PayloadHash || json.Unmarshal(raw, &event) != nil || json.Unmarshal(commandRaw, &command) != nil {
		return "", "", false, ErrJournalConflict
	}
	actualEvent, err := agentproto.RuntimeEventPayloadHash(event)
	sum := sha256.Sum256(commandRaw)
	if err != nil || actualEvent != hash || hex.EncodeToString(sum[:]) != commandHash {
		return "", "", false, ErrJournalConflict
	}
	switch command.Kind {
	case agentproto.CommandThreadRead, agentproto.CommandThreadHistoryRead, agentproto.CommandThreadGoalGet, agentproto.CommandMCPOAuthLogin:
	default:
		return "", "", false, nil
	}
	if command.Kind == agentproto.CommandMCPOAuthLogin && (command.MCP.OAuthLogin == nil || command.MCP.OAuthLogin.ThreadID != command.Target.ThreadID) {
		return "", "", false, ErrJournalConflict
	}
	outcome, _, ok := nativeEventOutcome(command, event)
	if !ok || outcome != status || sequence != int64(ref.Sequence) || command.NativeBinding == nil || command.NativeBinding.Owner != l.native || (command.Target.ThreadID == "" && command.Kind != agentproto.CommandMCPOAuthLogin) {
		return "", "", false, nil
	}
	var count int
	err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_local_receipts l JOIN private_effect e ON e.id=l.effect_id WHERE l.effect_id=? AND e.state='confirmed' AND e.receipt=l.receipt AND json_extract(l.receipt,'$.kind')='native_read_observation_expired' AND json_extract(l.receipt,'$.version')=1 AND json_extract(l.receipt,'$.dispatchId')=? AND json_extract(l.receipt,'$.commandId')=? AND json_extract(l.receipt,'$.instanceId')=? AND json_extract(l.receipt,'$.sessionId')=?`, strings.TrimSuffix(dispatch, ":native"), dispatch, command.CommandID, l.native.InstanceID, command.Target.ThreadID).Scan(&count)
	if err != nil {
		return "", "", false, err
	}
	return command.Target.ThreadID, dispatch, count == 1, nil
}
