package install

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type instancePortSet struct {
	Relay          int
	Admin          int
	Tool           int
	ExternalAccess int
	Pprof          int
}

func instanceDefaultPorts(instanceID string) instancePortSet {
	if normalizeInstanceID(instanceID) == debugInstanceID {
		return instancePortSet{
			Relay:          9600,
			Admin:          9601,
			Tool:           9602,
			ExternalAccess: 9612,
			Pprof:          17601,
		}
	}
	return instancePortSet{
		Relay:          9500,
		Admin:          9501,
		Tool:           9502,
		ExternalAccess: 9512,
		Pprof:          17501,
	}
}

func applyInstanceConfigDefaults(cfg *config.AppConfig, instanceID string, newConfig bool) {
	if cfg == nil || !newConfig {
		return
	}
	ports := instanceDefaultPorts(instanceID)
	cfg.Relay.ListenPort = ports.Relay
	cfg.Relay.ServerURL = relayServerURLForPort(ports.Relay)
	cfg.Admin.ListenPort = ports.Admin
	cfg.Tool.ListenPort = ports.Tool
	cfg.ExternalAccess.ListenPort = ports.ExternalAccess
	if cfg.Debug.Pprof == nil {
		cfg.Debug.Pprof = &config.PprofSettings{}
	}
	cfg.Debug.Pprof.ListenHost = firstNonEmpty(cfg.Debug.Pprof.ListenHost, "127.0.0.1")
	cfg.Debug.Pprof.ListenPort = ports.Pprof
}

func relayServerURLForPort(port int) string {
	return fmt.Sprintf("ws://127.0.0.1:%d/ws/agent", port)
}
