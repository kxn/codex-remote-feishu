package privatechat

import "context"

// ReadIngressAction returns the immutable already-admitted action for planning
// under the exact owner and generation; it does not mark the input applied.
func (j *Journal) ReadIngressAction(ctx context.Context, owner Owner, generation, sequence int64) (JournalEntry, error) {
	if j == nil || !validOwner(owner) || generation < 1 || sequence < 1 {
		return JournalEntry{}, ErrJournalState
	}
	entry, _, _, err := j.processorIngress(ctx, owner, generation, sequence)
	return entry, err
}
