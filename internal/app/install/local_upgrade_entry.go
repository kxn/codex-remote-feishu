package install

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func RunLocalUpgrade(args []string, _ io.Reader, stdout, _ io.Writer, _ string) error {
	defaults, err := DetectPlatformDefaults()
	if err != nil {
		return err
	}

	flagSet := flag.NewFlagSet("local-upgrade", flag.ContinueOnError)
	flagSet.SetOutput(stdout)

	instanceIDFlag := flagSet.String("instance", defaultInstanceID, "install instance: stable or debug")
	baseDir := flagSet.String("base-dir", defaults.BaseDir, "base directory for config and install state")
	statePathFlag := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir")
	slot := flagSet.String("slot", "", "slot label for the local upgrade; empty derives local-<fingerprint>")
	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	instanceID, err := parseInstanceID(*instanceIDFlag)
	if err != nil {
		return err
	}

	statePath := strings.TrimSpace(*statePathFlag)
	if statePath == "" {
		statePath = defaultInstallStatePathForInstance(*baseDir, instanceID)
	}
	stateValue, err := loadServiceState(statePath)
	if err != nil {
		return err
	}

	artifactPath := LocalUpgradeArtifactPath(stateValue)
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("local upgrade artifact is missing: %s", artifactPath)
	}

	helperBinary, err := resolveUpgradeHelperBinary(stateValue.StatePath)
	if err != nil {
		return err
	}
	resolvedSlot, err := RunLocalBinaryUpgradeWithStatePath(LocalBinaryUpgradeOptions{
		StatePath:    stateValue.StatePath,
		SourceBinary: artifactPath,
		Slot:         *slot,
		HelperBinary: helperBinary,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "local upgrade prepared from artifact: %s\nslot: %s\nstate: %s\n", artifactPath, resolvedSlot, stateValue.StatePath)
	return err
}
