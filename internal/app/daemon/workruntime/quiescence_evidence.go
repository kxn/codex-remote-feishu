package workruntime

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) persistQuiescenceEvidence(tx *workstore.Tx, request InspectionRequest, observation NativeObservation) (work.QuiescenceEvidence, error) {
	proofRef := "native-quiescence:" + request.Carrier.Incarnation + ":" + request.Binding.AttemptID
	data, err := json.Marshal(observation)
	if err != nil {
		return work.QuiescenceEvidence{}, err
	}
	if err := tx.PutContent(work.NewContent(proofRef, request.Binding.ScopeID, "application/json", data, observation.ObservedAt)); err != nil {
		return work.QuiescenceEvidence{}, err
	}
	proof := work.QuiescenceEvidence{RunRef: work.RunRef{WorkerID: request.Binding.WorkerSessionID, Generation: request.Binding.SessionGeneration, RunID: request.Binding.RunRef}, InstanceID: request.Carrier.OriginInstanceID, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, CarrierIncarnation: request.Carrier.Incarnation, NativeStopped: observation.NativeStopped, ContinuationStopped: observation.ContinuationStopped, BackgroundWorkStopped: observation.BackgroundWorkStopped, Source: work.SourceRef{Kind: "native_inspection", ID: proofRef, Version: "1", EvidenceRef: proofRef}, ObservedAt: observation.ObservedAt}
	return proof, nil
}
