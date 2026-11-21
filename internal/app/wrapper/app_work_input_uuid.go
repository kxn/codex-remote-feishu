package wrapper

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (r *claudeBackendRuntime) RegisterNativeUserUUID(uuid string, command agentproto.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translator.RegisterUserCommandUUID(uuid, command)
}

func (r *workExecutionRuntime) stampUserUUIDs(command agentproto.Command, result *runtimeCommandResult) error {
	if r.Backend() != agentproto.BackendClaude {
		return nil
	}
	registrar, ok := r.backendRuntime.(interface {
		RegisterNativeUserUUID(string, agentproto.Command)
	})
	if !ok {
		return nil
	}
	ordinal := 0
	for phaseIndex := range result.Phases {
		for frameIndex, frame := range result.Phases[phaseIndex].OutboundToChild {
			var payload map[string]any
			if json.Unmarshal(frame, &payload) != nil || payload["type"] != "user" {
				continue
			}
			seed := fmt.Sprintf("%s\x00%s\x00%s\x00%d", r.work.snapshot.Owner.WorkerSessionID, r.work.snapshot.CarrierIncarnation, command.CommandID, ordinal)
			ordinal++
			sum := sha256.Sum256([]byte(seed))
			sum[6] = (sum[6] & 15) | 0x50
			sum[8] = (sum[8] & 63) | 0x80
			uuid := fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
			payload["uuid"] = uuid
			encoded, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			result.Phases[phaseIndex].OutboundToChild[frameIndex] = append(encoded, '\n')
			registrar.RegisterNativeUserUUID(uuid, command)
		}
	}
	return nil
}
