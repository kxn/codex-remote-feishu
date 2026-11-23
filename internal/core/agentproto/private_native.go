package agentproto

import (
	"fmt"
	"path/filepath"
	"strings"
)

const PrivateNativeProtocolVersion = 1
const PrivateNativeCredentialEnv = "CODEX_REMOTE_PRIVATE_CARRIER_CREDENTIAL"
const PrivateNativeLaunchEnv = "CODEX_REMOTE_PRIVATE_NATIVE_LAUNCH"

type NativeOwner struct {
	Version            int     `json:"version"`
	Application        string  `json:"application"`
	OwnerID            string  `json:"ownerId"`
	Epoch              int64   `json:"epoch"`
	CarrierIncarnation string  `json:"carrierIncarnation"`
	InstanceID         string  `json:"instanceId"`
	HostID             string  `json:"hostId"`
	InstanceGeneration int64   `json:"instanceGeneration"`
	ProcessGeneration  int64   `json:"processGeneration"`
	Backend            Backend `json:"backend"`
	HistoryNamespace   string  `json:"historyNamespace"`
}
type NativeTurnBinding struct {
	Version     int         `json:"version"`
	Owner       NativeOwner `json:"owner"`
	ExecutionID string      `json:"executionId,omitempty"`
}
type PrivateNativeLaunchSnapshot struct {
	Version     int         `json:"version"`
	Owner       NativeOwner `json:"owner"`
	CWD         string      `json:"cwd"`
	JournalPath string      `json:"journalPath"`
}

func nativeCanonical(s string) bool { return s != "" && s == strings.TrimSpace(s) }
func (o NativeOwner) Validate() error {
	if o.Version != PrivateNativeProtocolVersion || o.Application != "private" || !nativeCanonical(o.OwnerID) || o.Epoch <= 0 || !nativeCanonical(o.CarrierIncarnation) || !nativeCanonical(o.InstanceID) || !nativeCanonical(o.HostID) || o.InstanceGeneration <= 0 || o.ProcessGeneration <= 0 || !nativeCanonical(o.HistoryNamespace) || (o.Backend != BackendCodex && o.Backend != BackendClaude && o.Backend != BackendOpenCode) {
		return fmt.Errorf("invalid private native owner")
	}
	return nil
}
func (b NativeTurnBinding) Validate(requireExecution bool) error {
	if b.Version != PrivateNativeProtocolVersion {
		return fmt.Errorf("unsupported native binding version")
	}
	if err := b.Owner.Validate(); err != nil {
		return err
	}
	if b.ExecutionID != "" && !nativeCanonical(b.ExecutionID) || requireExecution && b.ExecutionID == "" {
		return fmt.Errorf("native execution ID required")
	}
	return nil
}
func (s PrivateNativeLaunchSnapshot) Validate() error {
	if s.Version != PrivateNativeProtocolVersion {
		return fmt.Errorf("unsupported private launch version")
	}
	if err := s.Owner.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(s.CWD) || !filepath.IsAbs(s.JournalPath) {
		return fmt.Errorf("private launch requires absolute cwd and journal")
	}
	return nil
}
