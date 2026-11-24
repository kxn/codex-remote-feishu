package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"testing"
)

func TestHistoricalCommandRequiresExactCarrierGenerationAndHost(t *testing.T) {
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "front", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	cmd := workstore.NativeCommand{CommandID: "command", Incarnation: "old-carrier", Binding: owner}
	carrier := workstore.CarrierRegistration{Incarnation: "old-carrier", Owner: owner}
	process := agentproto.RuntimeProcessIdentity{HostID: "host", Platform: "linux", PID: 123, StartIdentity: "start"}
	if !matchesHistoricalCommandCarrier(cmd, carrier, process, "scope", "host") {
		t.Fatal("exact carrier rejected")
	}
	newer := carrier
	newer.Owner.SessionGeneration = 2
	if matchesHistoricalCommandCarrier(cmd, newer, process, "scope", "host") {
		t.Fatal("new generation used as old proof")
	}
	newer = carrier
	newer.Incarnation = "new-carrier"
	if matchesHistoricalCommandCarrier(cmd, newer, process, "scope", "host") {
		t.Fatal("different carrier used as proof")
	}
	newer = carrier
	newer.Owner.BotGeneration++
	if matchesHistoricalCommandCarrier(cmd, newer, process, "scope", "host") {
		t.Fatal("different bot generation used as proof")
	}
	if matchesHistoricalCommandCarrier(cmd, carrier, process, "scope", "other-host") {
		t.Fatal("foreign host used as proof")
	}
}
