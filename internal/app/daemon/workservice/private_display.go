package workservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PrivateDisplayPublication freezes the ordering and replacement ownership of
// one output of a durably bound private native turn.
type PrivateDisplayPublication struct {
	DisplayID        string `json:"display_id,omitempty"`
	SourceIngressID  string `json:"source_ingress_id,omitempty"`
	PreviousOutboxID string `json:"previous_outbox_id,omitempty"`
	AnchorOutboxID   string `json:"anchor_outbox_id,omitempty"`
	PatchFenceID     string `json:"patch_fence_id,omitempty"`
	Progress         bool   `json:"progress,omitempty"`
	ResultHash       string `json:"result_hash,omitempty"`
	ResultBodyRef    string `json:"result_body_ref,omitempty"`
}

func PrivateResultHash(body string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))))
	return hex.EncodeToString(hash[:])
}

// FindPrivateDisplayResult returns the first durable part of an equivalent
// native result already registered in the same execution run.
func FindPrivateDisplayResult(tx *workstore.Tx, scopeID, contextID, runRef, body string) (work.Operation, bool, error) {
	ops, err := tx.PrivateRunPublications(scopeID, contextID, runRef)
	if err != nil {
		return work.Operation{}, false, err
	}
	hash := PrivateResultHash(body)
	for _, op := range ops {
		projection, e := ReadMessageProjection(tx, op)
		if e != nil {
			return work.Operation{}, false, e
		}
		if projection.PrivateDisplay != nil && projection.PrivateDisplay.ResultHash == hash {
			return op, true, nil
		}
	}
	return work.Operation{}, false, nil
}

func ReadMessageProjection(tx *workstore.Tx, operation work.Operation) (MessageProjection, error) {
	var result MessageProjection
	body, err := tx.GetContent(operation.ScopeID, operation.RequestRef)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(body.Data, &result)
	return result, err
}
