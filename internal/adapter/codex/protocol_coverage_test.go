package codex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type protocolMethodSnapshot struct {
	Source       string                   `json:"source"`
	UpstreamHead string                   `json:"upstreamHead"`
	Methods      []protocolSnapshotMethod `json:"methods"`
}

type protocolSnapshotMethod struct {
	Direction ProtocolDirection `json:"direction"`
	Method    string            `json:"method"`
}

func TestProtocolCoverageManifestCoversCurrentUpstreamSnapshot(t *testing.T) {
	snapshot := loadProtocolMethodSnapshot(t)
	manifest := ProtocolCoverageManifest()
	coverage := map[string]ProtocolCoverageEntry{}
	for _, entry := range manifest {
		key := protocolCoverageKey(entry.Direction, entry.Method)
		if _, exists := coverage[key]; exists {
			t.Fatalf("duplicate protocol coverage entry for %s", key)
		}
		coverage[key] = entry
	}
	if len(snapshot.Methods) == 0 {
		t.Fatalf("snapshot has no methods")
	}
	for _, method := range snapshot.Methods {
		key := protocolCoverageKey(method.Direction, method.Method)
		if _, exists := coverage[key]; !exists {
			t.Fatalf("snapshot method %s missing from protocol coverage manifest", key)
		}
	}
	snapshotKeys := map[string]bool{}
	for _, method := range snapshot.Methods {
		key := protocolCoverageKey(method.Direction, method.Method)
		if snapshotKeys[key] {
			t.Fatalf("duplicate snapshot method %s", key)
		}
		snapshotKeys[key] = true
	}
	for _, entry := range manifest {
		key := protocolCoverageKey(entry.Direction, entry.Method)
		if !snapshotKeys[key] {
			t.Fatalf("coverage entry %s is not present in current upstream snapshot", key)
		}
	}
}

func TestProtocolCoverageManifestShape(t *testing.T) {
	validDirections := enumSet(
		ProtocolDirectionClientRequest,
		ProtocolDirectionServerRequest,
		ProtocolDirectionServerNotification,
	)
	validTargets := enumSet(
		ProtocolTargetPassThroughOnly,
		ProtocolTargetCanonicalized,
		ProtocolTargetStateOnly,
		ProtocolTargetProductVisible,
		ProtocolTargetHeadlessDriven,
		ProtocolTargetUnsupportedFailClosed,
	)
	validStatuses := enumSet(
		ProtocolStatusSupported,
		ProtocolStatusPartial,
		ProtocolStatusPlanned,
		ProtocolStatusUnsupported,
		ProtocolStatusNativeOnly,
		ProtocolStatusDeprecated,
	)
	validCadences := enumSet(
		ProtocolCadenceEvent,
		ProtocolCadenceSnapshot,
		ProtocolCadenceStreamDelta,
		ProtocolCadenceHighFrequency,
		ProtocolCadenceRequest,
		ProtocolCadenceResponse,
	)
	validProjectionPolicies := enumSet(
		FeishuProjectionIgnore,
		FeishuProjectionFinalOnly,
		FeishuProjectionLatestOnly,
		FeishuProjectionCoalesced,
		FeishuProjectionStateOnly,
		FeishuProjectionPassThroughOnly,
		FeishuProjectionProductVisible,
		FeishuProjectionUnsupportedFailClosed,
	)

	for _, entry := range ProtocolCoverageManifest() {
		key := protocolCoverageKey(entry.Direction, entry.Method)
		if strings.TrimSpace(entry.Method) == "" {
			t.Fatalf("coverage entry has empty method: %#v", entry)
		}
		if !validDirections[entry.Direction] {
			t.Fatalf("%s has invalid direction %q", key, entry.Direction)
		}
		if !validTargets[entry.TargetLayer] {
			t.Fatalf("%s has invalid target layer %q", key, entry.TargetLayer)
		}
		if !validStatuses[entry.Status] {
			t.Fatalf("%s has invalid status %q", key, entry.Status)
		}
		if !validCadences[entry.Cadence] {
			t.Fatalf("%s has invalid cadence %q", key, entry.Cadence)
		}
		if !validProjectionPolicies[entry.FeishuProjectionPolicy] {
			t.Fatalf("%s has invalid Feishu projection policy %q", key, entry.FeishuProjectionPolicy)
		}
		if strings.TrimSpace(entry.Owner) == "" {
			t.Fatalf("%s has empty owner", key)
		}
		if strings.TrimSpace(entry.Notes) == "" {
			t.Fatalf("%s has empty notes", key)
		}
		if entry.Cadence == ProtocolCadenceStreamDelta || entry.Cadence == ProtocolCadenceHighFrequency {
			if entry.AuthoritativeFinal == "" && !entry.NativePassThroughRequired {
				t.Fatalf("%s is streaming/high-frequency but has no authoritative final or native pass-through requirement", key)
			}
			switch entry.FeishuProjectionPolicy {
			case FeishuProjectionFinalOnly, FeishuProjectionCoalesced, FeishuProjectionPassThroughOnly, FeishuProjectionIgnore:
			default:
				t.Fatalf("%s has unsafe projection policy %q for streaming/high-frequency method", key, entry.FeishuProjectionPolicy)
			}
		}
		if entry.HeadlessOptOutCandidate && entry.Cadence != ProtocolCadenceStreamDelta && entry.Cadence != ProtocolCadenceHighFrequency {
			t.Fatalf("%s is an opt-out candidate without streaming/high-frequency cadence", key)
		}
		if entry.Direction == ProtocolDirectionServerRequest {
			switch entry.TargetLayer {
			case ProtocolTargetCanonicalized, ProtocolTargetPassThroughOnly, ProtocolTargetUnsupportedFailClosed:
			default:
				t.Fatalf("%s server request has unsafe target layer %q", key, entry.TargetLayer)
			}
			if entry.TargetLayer == ProtocolTargetUnsupportedFailClosed && entry.FeishuProjectionPolicy != FeishuProjectionUnsupportedFailClosed {
				t.Fatalf("%s fail-closed request has projection policy %q", key, entry.FeishuProjectionPolicy)
			}
		}
		if entry.NativePassThroughRequired && entry.FeishuProjectionPolicy != FeishuProjectionPassThroughOnly {
			t.Fatalf("%s requires native pass-through but projection policy is %q", key, entry.FeishuProjectionPolicy)
		}
	}
}

func loadProtocolMethodSnapshot(t *testing.T) protocolMethodSnapshot {
	t.Helper()
	raw, err := os.ReadFile("testdata/app_server_protocol_methods_current.json")
	if err != nil {
		t.Fatalf("read protocol method snapshot: %v", err)
	}
	var snapshot protocolMethodSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode protocol method snapshot: %v", err)
	}
	if snapshot.Source != "openai/codex" {
		t.Fatalf("unexpected snapshot source %q", snapshot.Source)
	}
	if strings.TrimSpace(snapshot.UpstreamHead) == "" {
		t.Fatalf("snapshot missing upstream head")
	}
	return snapshot
}

func protocolCoverageKey(direction ProtocolDirection, method string) string {
	return string(direction) + ":" + method
}

func enumSet[T ~string](values ...T) map[T]bool {
	result := make(map[T]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}
