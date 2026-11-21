package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// RequireActiveAgreement is for newly adopted decisions. Historical pending
// requests retain their frozen version until an explicit migration applies.
func (t *Tx) RequireActiveAgreement(scopeID, id string, revision int64, now time.Time) (work.ParticipationAgreement, error) {
	agreement, err := t.GetAgreement(scopeID, id)
	if err != nil {
		return agreement, err
	}
	if agreement.Revision != revision || agreement.ValidFrom.After(now) || (agreement.ValidUntil != nil && !agreement.ValidUntil.After(now)) {
		return agreement, fmt.Errorf("decision must adopt the current effective participation agreement")
	}
	agreements, err := t.ListAgreements(scopeID)
	if err != nil {
		return agreement, err
	}
	for _, other := range agreements {
		if other.SupersedesID == id && !other.ValidFrom.After(now) && (other.ValidUntil == nil || other.ValidUntil.After(now)) {
			return agreement, fmt.Errorf("participation agreement was superseded")
		}
	}
	return agreement, nil
}
