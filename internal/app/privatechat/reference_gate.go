package privatechat

import "sync"

// SetReferenceGate is called once during assembly before processor or import
// work starts. The host holds its write side across profile reference scans and
// config mutation; private mutations hold the read side before processor locks.
func (j *Journal) SetReferenceGate(gate *sync.RWMutex) { j.referenceGate = gate }
func (j *Journal) LockReferenceMutation() func() {
	if j.referenceGate == nil {
		return func() {}
	}
	j.referenceGate.RLock()
	return j.referenceGate.RUnlock
}
