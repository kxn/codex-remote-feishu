package install

import (
	"fmt"
	"hash/fnv"

	"github.com/kxn/codex-remote-feishu/internal/config"
)

type instancePortSet struct {
	Relay          int
	Admin          int
	Tool           int
	ExternalAccess int
	Pprof          int
}

type instancePortTemplate struct {
	RelayBase   int
	PprofBase   int
	StartOffset int
}

const (
	instancePortBundleStep       = 20
	instancePortCandidateLimit   = 200
	defaultRelayPortBase         = 9500
	defaultPprofPortBase         = 17501
	nonDefaultRelayPortBase      = 9600
	nonDefaultPprofPortBase      = 17601
	nonDefaultPortOffsetVariants = 50
)

func instanceDefaultPorts(instanceID string) instancePortSet {
	return instancePortCandidate(instancePortTemplateFor(instanceID), 0)
}

func selectAvailablePortSet(instanceID string) (instancePortSet, error) {
	template := instancePortTemplateFor(instanceID)
	for i := 0; i < instancePortCandidateLimit; i++ {
		candidate := instancePortCandidate(template, i)
		if portSetAvailable(candidate) {
			return candidate, nil
		}
	}
	return instancePortSet{}, fmt.Errorf("unable to allocate available ports for instance %q", normalizeInstanceID(instanceID))
}

func instancePortTemplateFor(instanceID string) instancePortTemplate {
	instanceID = normalizeInstanceID(instanceID)
	if isDefaultInstance(instanceID) {
		return instancePortTemplate{
			RelayBase: defaultRelayPortBase,
			PprofBase: defaultPprofPortBase,
		}
	}
	template := instancePortTemplate{
		RelayBase: nonDefaultRelayPortBase,
		PprofBase: nonDefaultPprofPortBase,
	}
	if instanceID == debugInstanceID {
		return template
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(instanceID))
	template.StartOffset = int(hash.Sum32() % nonDefaultPortOffsetVariants)
	return template
}

func instancePortCandidate(template instancePortTemplate, attempt int) instancePortSet {
	offset := (template.StartOffset + attempt) * instancePortBundleStep
	relayBase := template.RelayBase + offset
	return instancePortSet{
		Relay:          relayBase,
		Admin:          relayBase + 1,
		Tool:           relayBase + 2,
		ExternalAccess: relayBase + 12,
		Pprof:          template.PprofBase + offset,
	}
}

func applyInstanceConfigDefaults(cfg *config.AppConfig, instanceID string, newConfig bool) error {
	if cfg == nil || !newConfig {
		return nil
	}
	ports, err := selectAvailablePortSet(instanceID)
	if err != nil {
		return err
	}
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
	applyBuildFlavorDebugDefaults(cfg)
	return nil
}

func relayServerURLForPort(port int) string {
	return fmt.Sprintf("ws://127.0.0.1:%d/ws/agent", port)
}
