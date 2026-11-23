package nativeinspection

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
	"time"
)

func TestPrivateOwnerQuiescenceRequiresEveryDomain(t *testing.T) {
	o := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "i", HostID: "h", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	probe := func(req, res string) agentproto.RPCProbe {
		return agentproto.RPCProbe{Request: json.RawMessage(req), Response: json.RawMessage(res)}
	}
	r := agentproto.RuntimeInspectionResult{Version: 1, CarrierIncarnation: "inc", InstanceGeneration: 1, ProcessGeneration: 1, ObservedAt: time.Now(), OwnerScope: &agentproto.RuntimeOwnerInspection{KnownSessionsComplete: true, Bootstrap: probe(`{"id":"init","method":"initialize"}`, `{"id":"init","result":{}}`), Sessions: []agentproto.RuntimeSessionInspection{{SessionID: "s", Native: probe(`{"id":"n","method":"thread/read","params":{"threadId":"s"}}`, `{"id":"n","result":{"thread":{"id":"s","status":{"type":"idle"}}}}`), Continuation: probe(`{"id":"g","method":"thread/goal/get","params":{"threadId":"s"}}`, `{"id":"g","result":{"goal":null}}`)}}}, Background: agentproto.BackgroundInspection{CarrierIncarnation: "inc", ProcessGeneration: 1, Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"source":"host"}`)}}
	if err := VerifyPrivateOwnerStopped(o, r); err != nil {
		t.Fatal(err)
	}
	r.Background.PendingNativeInputs = 1
	if VerifyPrivateOwnerStopped(o, r) == nil {
		t.Fatal("pending input accepted")
	}
	r.Background.PendingNativeInputs = 0
	r.OwnerScope.Sessions[0].Continuation.Response = json.RawMessage(`{"id":"g","result":{"goal":{"status":"active"}}}`)
	if VerifyPrivateOwnerStopped(o, r) == nil {
		t.Fatal("active goal accepted")
	}
	r.OwnerScope.Sessions[0].Continuation.Response = json.RawMessage(`{"id":"bad","result":{"goal":null}}`)
	if VerifyPrivateOwnerStopped(o, r) == nil {
		t.Fatal("uncorrelated result accepted")
	}
}
