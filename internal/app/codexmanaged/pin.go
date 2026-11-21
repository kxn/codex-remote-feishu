package codexmanaged

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type exactPin struct {
	Binary  string `json:"binary"`
	Version string `json:"version"`
}

// PinInstalledRelease freezes a verified, already installed managed release.
// Installation is performed by the durable tracked-upgrade coordinator.
func PinInstalledRelease(ctx context.Context, root, prefix, target string) (string, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(prefix) {
		return "", fmt.Errorf("managed pin requires explicit host paths")
	}
	binary, err := installedBinary(prefix)
	if err != nil {
		return "", err
	}
	actual, err := readBinaryVersion(ctx, binary)
	if err != nil {
		return "", err
	}
	parsed, err := parseVersion(actual)
	if err != nil || parsed.text != target {
		return "", fmt.Errorf("managed binary version differs from target")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	body, _ := json.Marshal(exactPin{Binary: binary, Version: target})
	temp, err := os.CreateTemp(root, ".pin-")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(body)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = os.Rename(name, filepath.Join(root, "exact-version.json")); err != nil {
		return "", err
	}
	return binary, nil
}
func readExactPin(ctx context.Context, root string, ops operations) (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, "exact-version.json"))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	var pin exactPin
	if err = json.Unmarshal(raw, &pin); err != nil {
		return "", true, err
	}
	if !filepath.IsAbs(pin.Binary) || pin.Version == "" {
		return "", true, fmt.Errorf("invalid managed Codex pin")
	}
	verified, err := verifyCandidate(ctx, pin.Binary, ops)
	if err != nil {
		return "", true, err
	}
	if verified.version.text != pin.Version {
		return "", true, fmt.Errorf("pinned Codex version changed")
	}
	return pin.Binary, true, nil
}

func VerifyExactBinaryWithEnv(ctx context.Context, binary, target string, env []string) error {
	if !filepath.IsAbs(binary) || target == "" {
		return fmt.Errorf("exact Codex release requires host path and version")
	}
	text, err := readBinaryVersionWithEnv(ctx, binary, env)
	if err != nil {
		return err
	}
	parsed, err := parseVersion(text)
	if err != nil || parsed.text != target {
		return fmt.Errorf("actual managed Codex binary differs from frozen release")
	}
	return nil
}

func ResolveVerifiedConfiguredBinary(ctx context.Context, configured string, env []string) (string, string, error) {
	binary, err := configuredCodexBinary(configured, env)
	if err != nil {
		return "", "", err
	}
	version, err := readBinaryVersionWithEnv(ctx, binary, env)
	if err != nil {
		return "", "", err
	}
	if _, err = parseVersion(version); err != nil {
		return "", "", err
	}
	return binary, version, nil
}
