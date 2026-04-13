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

	instanceIDFlag := flagSet.String("instance", "", "install instance id; empty uses the workspace binding when available")
	baseDir := flagSet.String("base-dir", "", "base directory for config and install state; empty auto-resolves to workspace binding or platform default")
	statePathFlag := flagSet.String("state-path", "", "path to install-state.json; empty derives from -base-dir")
	slot := flagSet.String("slot", "", "slot label for the local upgrade; empty derives local-<fingerprint>")
	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	statePath := strings.TrimSpace(*statePathFlag)
	if statePath == "" {
		explicitInstance := strings.TrimSpace(*instanceIDFlag)
		explicitBaseDir := strings.TrimSpace(*baseDir)
		if explicitInstance == "" && explicitBaseDir == "" {
			info, err := ResolveRepoInstallTargetInfo(RepoInstallTargetOptions{
				FallbackBaseDir: defaults.BaseDir,
				GOOS:            defaults.GOOS,
				RequireBinding:  true,
			})
			if err != nil {
				return fmt.Errorf("local-upgrade requires a bound repo target or explicit -instance/-base-dir/-state-path; prefer ./upgrade-local.sh: %w", err)
			}
			statePath = info.StatePath
		} else {
			selection, err := resolveInstallInstanceSelection(explicitInstance, explicitBaseDir, defaults.BaseDir, defaults.GOOS)
			if err != nil {
				return err
			}
			statePath = selection.StatePath
		}
	}
	stateValue, err := loadServiceState(statePath)
	if err != nil {
		return err
	}

	artifactPath := LocalUpgradeArtifactPath(stateValue)
	if _, err := os.Stat(artifactPath); err != nil {
		return fmt.Errorf("local upgrade artifact is missing: %s", artifactPath)
	}

	resolvedSlot, err := RunLocalBinaryUpgradeWithStatePath(LocalBinaryUpgradeOptions{
		StatePath:    stateValue.StatePath,
		SourceBinary: artifactPath,
		Slot:         *slot,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "local upgrade prepared from artifact: %s\nslot: %s\nstate: %s\n", artifactPath, resolvedSlot, stateValue.StatePath)
	return err
}
