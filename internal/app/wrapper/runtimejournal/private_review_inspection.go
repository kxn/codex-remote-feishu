package runtimejournal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
)

// PrivateEphemeralReviewProof returns only references. The sensitive native
// start frame stays inside the owner-only wrapper journal.
func (s *Store) PrivateEphemeralReviewProof(owner agentproto.NativeOwner, session string) (*agentproto.RuntimeEphemeralReviewProof, error) {
	rows, err := s.db.Query(`SELECT c.payload,f.id,f.hash,p.payload,reviewframe.hash,reviewpayload.payload,created.payload,created.hash,created.sequence,accepted.payload,accepted.hash,accepted.sequence,terminal.payload,terminal.hash,terminal.sequence FROM commands c JOIN frames f ON f.command_id=c.id AND f.native_method='thread/start' JOIN private_review_frames p ON p.frame_id=f.id JOIN events created ON created.incarnation=c.incarnation AND json_extract(created.payload,'$.kind')=? AND json_extract(created.payload,'$.commandId')=c.id AND json_extract(created.payload,'$.threadId')=? AND json_extract(created.payload,'$.requestId')=f.native_id JOIN events accepted ON accepted.incarnation=c.incarnation AND accepted.sequence>created.sequence AND json_extract(accepted.payload,'$.kind')=? AND json_extract(accepted.payload,'$.commandId')=c.id AND json_extract(accepted.payload,'$.action')='review.start' AND json_extract(accepted.payload,'$.status')='accepted' AND json_extract(accepted.payload,'$.metadata.reviewThreadID')=? JOIN frames reviewframe ON reviewframe.command_id=c.id AND reviewframe.native_method='review/start' AND reviewframe.native_id=json_extract(accepted.payload,'$.requestId') AND reviewframe.stage IN ('written','child_write_started','unknown') JOIN private_review_frames reviewpayload ON reviewpayload.frame_id=reviewframe.id JOIN events terminal ON terminal.incarnation=c.incarnation AND terminal.sequence>accepted.sequence AND json_extract(terminal.payload,'$.kind')=? AND json_extract(terminal.payload,'$.threadId')=? AND json_extract(terminal.payload,'$.turnId')=json_extract(accepted.payload,'$.turnId') WHERE c.incarnation=? AND f.stage IN ('written','child_write_started','unknown')`, string(agentproto.EventReviewChildCreated), session, string(agentproto.EventNativeControlResult), session, string(agentproto.EventTurnCompleted), session, s.incarnation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result *agentproto.RuntimeEphemeralReviewProof
	for rows.Next() {
		var commandRaw, frame, reviewFrame, createdRaw, acceptedRaw, terminalRaw []byte
		var frameHash, reviewFrameHash, ch, ah, th string
		var f int64
		var cs, as, ts uint64
		if err = rows.Scan(&commandRaw, &f, &frameHash, &frame, &reviewFrameHash, &reviewFrame, &createdRaw, &ch, &cs, &acceptedRaw, &ah, &as, &terminalRaw, &th, &ts); err != nil {
			return nil, err
		}
		var c agentproto.Command
		var created, accepted, terminal agentproto.Event
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Ephemeral bool   `json:"ephemeral"`
				CWD       string `json:"cwd"`
			} `json:"params"`
		}
		var reviewReq struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				ThreadID string `json:"threadId"`
				Delivery string `json:"delivery"`
			} `json:"params"`
		}
		if json.Unmarshal(commandRaw, &c) != nil || json.Unmarshal(frame, &req) != nil || json.Unmarshal(reviewFrame, &reviewReq) != nil || hash(reviewFrame) != reviewFrameHash || reviewReq.Method != "review/start" || reviewReq.Params.ThreadID != session || reviewReq.Params.Delivery != "inline" || json.Unmarshal(createdRaw, &created) != nil || json.Unmarshal(acceptedRaw, &accepted) != nil || fmt.Sprint(reviewReq.ID) != accepted.RequestID || json.Unmarshal(terminalRaw, &terminal) != nil || c.Kind != agentproto.CommandReviewStart || c.NativeBinding == nil || c.NativeBinding.Owner != owner || c.CommandID == "" || hash(frame) != frameHash || req.Method != "thread/start" || !req.Params.Ephemeral || c.Target.CWD == "" || !pathcompare.SameCleanPlatformPath(req.Params.CWD, c.Target.CWD) || fmt.Sprint(req.ID) != created.RequestID || created.CommandID != c.CommandID || created.NativeBinding == nil || *created.NativeBinding != *c.NativeBinding || created.Metadata["parentThreadID"] != c.Target.ThreadID || created.Metadata["stage"] != "thread/start" || accepted.CommandID != c.CommandID || accepted.ThreadID != c.Target.ThreadID || accepted.NativeBinding == nil || *accepted.NativeBinding != *c.NativeBinding || accepted.RequestID == "" || terminal.TurnID == "" || (terminal.Status != "completed" && terminal.Status != "failed" && terminal.Status != "interrupted") {
			return nil, ErrConflict
		}
		for _, pair := range []struct {
			e agentproto.Event
			h string
		}{{created, ch}, {accepted, ah}, {terminal, th}} {
			digest, e := agentproto.RuntimeEventPayloadHash(pair.e)
			if e != nil || digest != pair.h {
				return nil, ErrConflict
			}
		}
		if result != nil {
			return nil, ErrConflict
		}
		result = &agentproto.RuntimeEphemeralReviewProof{CommandID: c.CommandID, FrameID: f, FrameHash: frameHash, CreatedSequence: cs, AcceptedSequence: as, TerminalSequence: ts}
	}
	return result, rows.Err()
}

func ReadPrivateEphemeralReviewProof(ctx context.Context, snapshot agentproto.PrivateNativeLaunchSnapshot, stateDir, session string) (*agentproto.RuntimeEphemeralReviewProof, error) {
	if snapshot.Validate() != nil {
		return nil, ErrConflict
	}
	path, err := ValidatePath(snapshot.JournalPath, stateDir, snapshot.CWD)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	raw, _ := json.Marshal(snapshot)
	var launch string
	if err = db.QueryRowContext(ctx, `SELECT payload_hash FROM launch_contracts WHERE incarnation=?`, snapshot.Owner.CarrierIncarnation).Scan(&launch); err != nil {
		return nil, err
	}
	if launch != hash(raw) {
		return nil, ErrConflict
	}
	s := &Store{db: db, incarnation: snapshot.Owner.CarrierIncarnation}
	return s.PrivateEphemeralReviewProof(snapshot.Owner, session)
}
