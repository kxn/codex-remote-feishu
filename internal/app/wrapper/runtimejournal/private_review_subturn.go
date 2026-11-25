package runtimejournal

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// PrivateReviewSubturnClosed proves a native subturn's root review completed.
// It does not create a terminal event for the subturn.
func (s *Store) PrivateReviewSubturnClosed(session, turn string) (bool, error) {
	if session == "" || turn == "" {
		return false, nil
	}
	binding, err := s.PrivateBindingForTurn(session, turn)
	if err != nil || binding == nil {
		return false, err
	}
	var n int
	err = s.db.QueryRow(`SELECT count(*) FROM events started JOIN events accepted ON accepted.incarnation=started.incarnation AND json_extract(accepted.payload,'$.kind')=? AND json_extract(accepted.payload,'$.action')='review.start' AND json_extract(accepted.payload,'$.status')='accepted' AND json_extract(accepted.payload,'$.metadata.reviewThreadID')=? AND json_extract(accepted.payload,'$.turnId')=json_extract(started.payload,'$.rootTurnId') JOIN events terminal ON terminal.incarnation=started.incarnation AND terminal.sequence>started.sequence AND json_extract(terminal.payload,'$.kind')=? AND json_extract(terminal.payload,'$.threadId')=? AND json_extract(terminal.payload,'$.turnId')=json_extract(started.payload,'$.rootTurnId') WHERE started.incarnation=? AND json_extract(started.payload,'$.kind')=? AND json_extract(started.payload,'$.threadId')=? AND json_extract(started.payload,'$.turnId')=? AND coalesce(json_extract(started.payload,'$.rootTurnId'),'')<>'' AND json_extract(started.payload,'$.rootTurnId')<>?`, string(agentproto.EventNativeControlResult), session, string(agentproto.EventTurnCompleted), session, s.incarnation, string(agentproto.EventTurnStarted), session, turn, turn).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 1 {
		return false, ErrConflict
	}
	return n == 1, nil
}
