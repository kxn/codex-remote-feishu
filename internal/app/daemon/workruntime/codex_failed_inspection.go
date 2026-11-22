package workruntime

type codexInspectionTurn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Codex keeps a thread in systemError after a failed turn (for example an
// unsupported model). That is not an active turn. Require the exact failed
// turn and a complete terminal turn list, in addition to the caller's separate
// host, queued-input and continuation proofs; the status alone proves nothing.
func codexFailedTurnStopped(turnID string, turns []codexInspectionTurn) bool {
	if turnID == "" {
		return false
	}
	found := false
	for _, turn := range turns {
		switch turn.Status {
		case "completed", "failed", "interrupted":
		default:
			return false
		}
		if turn.ID == turnID {
			if found || turn.Status != "failed" {
				return false
			}
			found = true
		}
	}
	return found
}
