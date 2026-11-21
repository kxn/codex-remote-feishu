package work

import (
	"fmt"
	"time"
)

// QuiescenceEvidence is produced by a trusted runtime reconciliation adapter,
// after checking the native run, continuation and background work separately.
// Lease expiry, disconnect and wall-clock timeout cannot construct this proof.
type QuiescenceEvidence struct {
	RunRef                RunRef    `json:"run_ref"`
	InstanceID            string    `json:"instance_id"`
	InstanceGeneration    int64     `json:"instance_generation"`
	ProcessGeneration     int64     `json:"process_generation"`
	CarrierIncarnation    string    `json:"carrier_incarnation"`
	NativeStopped         bool      `json:"native_stopped"`
	ContinuationStopped   bool      `json:"continuation_stopped"`
	BackgroundWorkStopped bool      `json:"background_work_stopped"`
	Source                SourceRef `json:"source"`
	ObservedAt            time.Time `json:"observed_at"`
}

func (v QuiescenceEvidence) Validate() error {
	if err := v.RunRef.Validate(); err != nil {
		return err
	}
	if err := required(v.InstanceID); err != nil {
		return err
	}
	if err := positive("quiescence instance generation", v.InstanceGeneration); err != nil {
		return err
	}
	if err := positive("quiescence process generation", v.ProcessGeneration); err != nil {
		return err
	}
	if err := v.Source.Validate(); err != nil {
		return err
	}
	if !v.NativeStopped || !v.ContinuationStopped || !v.BackgroundWorkStopped || v.ObservedAt.IsZero() {
		return fmt.Errorf("quiescence requires explicit native, continuation and background stop evidence")
	}
	return nil
}
