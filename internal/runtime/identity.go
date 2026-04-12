package relayruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const ProductName = "codex-remote"

func CurrentBinaryIdentity(version string) (agentproto.BinaryIdentity, error) {
	return CurrentBinaryIdentityWithBranch(version, "")
}

func CurrentBinaryIdentityWithBranch(version, branch string) (agentproto.BinaryIdentity, error) {
	executable, err := os.Executable()
	if err != nil {
		return agentproto.BinaryIdentity{}, err
	}
	return BinaryIdentityForPathWithBranch(executable, version, branch)
}

func BinaryIdentityForPath(executable, version string) (agentproto.BinaryIdentity, error) {
	return BinaryIdentityForPathWithBranch(executable, version, "")
}

func BinaryIdentityForPathWithBranch(executable, version, branch string) (agentproto.BinaryIdentity, error) {
	executable = filepath.Clean(executable)
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		executable = filepath.Clean(executable)
	}
	fingerprint, err := fileFingerprint(executable)
	if err != nil {
		return agentproto.BinaryIdentity{}, err
	}
	return agentproto.BinaryIdentity{
		Product:          ProductName,
		Version:          strings.TrimSpace(version),
		Branch:           strings.TrimSpace(branch),
		BuildFingerprint: fingerprint,
		BinaryPath:       executable,
	}, nil
}

func NewServerIdentity(version, configPath string, startedAt time.Time) (agentproto.ServerIdentity, error) {
	return NewServerIdentityWithBranch(version, "", configPath, startedAt)
}

func NewServerIdentityWithBranch(version, branch, configPath string, startedAt time.Time) (agentproto.ServerIdentity, error) {
	binaryIdentity, err := CurrentBinaryIdentityWithBranch(version, branch)
	if err != nil {
		return agentproto.ServerIdentity{}, err
	}
	return agentproto.ServerIdentity{
		BinaryIdentity: binaryIdentity,
		PID:            os.Getpid(),
		StartedAt:      startedAt,
		ConfigPath:     configPath,
	}, nil
}

func CompatibleIdentity(local agentproto.BinaryIdentity, remote agentproto.BinaryIdentity) bool {
	if remote.Product != "" && remote.Product != ProductName {
		return false
	}
	if local.BuildFingerprint != "" && remote.BuildFingerprint != "" {
		return local.BuildFingerprint == remote.BuildFingerprint
	}
	if local.Version != "" && remote.Version != "" {
		return local.Version == remote.Version
	}
	return false
}

func WriteServerIdentity(path string, identity agentproto.ServerIdentity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func ReadServerIdentity(path string) (agentproto.ServerIdentity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return agentproto.ServerIdentity{}, err
	}
	var identity agentproto.ServerIdentity
	if err := json.Unmarshal(raw, &identity); err != nil {
		return agentproto.ServerIdentity{}, err
	}
	return identity, nil
}

func WritePID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func ReadPID(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(raw), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

func fileFingerprint(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}
