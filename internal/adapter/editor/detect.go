package editor

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/managedshim"
	managedshimembed "github.com/kxn/codex-remote-feishu/internal/managedshim/embed"
)

const (
	ManagedShimKindTiny   = "tiny_shim"
	ManagedShimKindLegacy = "legacy_copied_binary"
)

type VSCodeSettingsStatus struct {
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	CLIExecutable string `json:"cliExecutable,omitempty"`
	ParseError    string `json:"parseError,omitempty"`
	MatchesBinary bool   `json:"matchesBinary"`
}

type ManagedShimStatus struct {
	Entrypoint              string `json:"entrypoint"`
	Exists                  bool   `json:"exists"`
	Kind                    string `json:"kind,omitempty"`
	RepoManaged             bool   `json:"repoManaged"`
	RealBinaryPath          string `json:"realBinaryPath,omitempty"`
	RealBinaryExists        bool   `json:"realBinaryExists"`
	SidecarPath             string `json:"sidecarPath,omitempty"`
	SidecarExists           bool   `json:"sidecarExists"`
	SidecarValid            bool   `json:"sidecarValid"`
	SidecarInstallStatePath string `json:"sidecarInstallStatePath,omitempty"`
	SidecarConfigPath       string `json:"sidecarConfigPath,omitempty"`
	SidecarInstanceID       string `json:"sidecarInstanceId,omitempty"`
	Installed               bool   `json:"installed"`
	MatchesBinary           bool   `json:"matchesBinary"`
}

func DetectVSCodeSettings(settingsPath, executable string) (VSCodeSettingsStatus, error) {
	status := VSCodeSettingsStatus{Path: settingsPath}
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, err
	}
	status.Exists = true

	settings, err := decodeVSCodeSettings(raw)
	if err != nil {
		status.ParseError = err.Error()
		return status, nil
	}
	value, _ := settings["chatgpt.cliExecutable"].(string)
	status.CLIExecutable = strings.TrimSpace(value)
	status.MatchesBinary = sameCleanPath(status.CLIExecutable, executable)
	return status, nil
}

func DetectManagedShim(entrypointPath, wrapperBinary string) (ManagedShimStatus, error) {
	status := ManagedShimStatus{
		Entrypoint:     entrypointPath,
		RealBinaryPath: ManagedShimRealBinaryPath(entrypointPath),
		SidecarPath:    ManagedShimSidecarPath(entrypointPath),
	}
	if strings.TrimSpace(entrypointPath) == "" {
		return status, nil
	}

	if _, err := os.Stat(entrypointPath); err == nil {
		status.Exists = true
	} else if !os.IsNotExist(err) {
		return status, err
	}
	if _, err := os.Stat(status.RealBinaryPath); err == nil {
		status.RealBinaryExists = true
	} else if !os.IsNotExist(err) {
		return status, err
	}
	if _, err := os.Stat(status.SidecarPath); err == nil {
		status.SidecarExists = true
	} else if !os.IsNotExist(err) {
		return status, err
	}

	switch {
	case status.SidecarExists:
		status.Kind = ManagedShimKindTiny
		status.RepoManaged = true
		sidecar, err := managedshim.ReadSidecar(status.SidecarPath)
		if err == nil && managedshim.SidecarValid(sidecar) {
			status.SidecarValid = true
			status.SidecarInstallStatePath = sidecar.InstallStatePath
			status.SidecarConfigPath = sidecar.ConfigPath
			status.SidecarInstanceID = sidecar.InstanceID
		}
		status.Installed = status.Exists && status.RealBinaryExists && status.SidecarValid
		status.MatchesBinary = matchesEmbeddedManagedShim(entrypointPath)
	case status.Exists && status.RealBinaryExists:
		status.Kind = ManagedShimKindLegacy
		status.RepoManaged = true
		status.Installed = true
		if strings.TrimSpace(wrapperBinary) != "" {
			matches, err := sameFileContents(entrypointPath, wrapperBinary)
			if err != nil {
				return status, err
			}
			status.MatchesBinary = matches
		}
	}
	return status, nil
}

func matchesEmbeddedManagedShim(path string) bool {
	expected := managedshimembed.ExpectedSHA256()
	if strings.TrimSpace(expected) == "" {
		return false
	}
	got, err := fileDigestHex(path)
	if err != nil {
		return false
	}
	return got == expected
}

func sameCleanPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameFileContents(leftPath, rightPath string) (bool, error) {
	left, err := fileDigest(leftPath)
	if err != nil {
		return false, err
	}
	right, err := fileDigest(rightPath)
	if err != nil {
		return false, err
	}
	return left == right, nil
}

func fileDigest(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func fileDigestHex(path string) (string, error) {
	digest, err := fileDigest(path)
	if err != nil {
		return "", err
	}
	return fmtDigest(digest), nil
}

func fmtDigest(digest [32]byte) string {
	const hex = "0123456789abcdef"
	buf := make([]byte, len(digest)*2)
	for i, b := range digest {
		buf[i*2] = hex[b>>4]
		buf[i*2+1] = hex[b&0x0f]
	}
	return string(buf)
}
