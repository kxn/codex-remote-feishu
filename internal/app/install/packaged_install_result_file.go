package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const packagedInstallResultSection = "result"

func writePackagedInstallResultFile(path string, result PackagedInstallResult) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if strings.TrimSpace(dir) != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	var builder strings.Builder
	builder.WriteString("[" + packagedInstallResultSection + "]\n")
	writePackagedInstallResultLine(&builder, "ok", boolString(result.OK))
	writePackagedInstallResultLine(&builder, "mode", result.Mode)
	writePackagedInstallResultLine(&builder, "statePath", result.StatePath)
	writePackagedInstallResultLine(&builder, "configPath", result.ConfigPath)
	writePackagedInstallResultLine(&builder, "installedBinary", result.InstalledBinary)
	writePackagedInstallResultLine(&builder, "serviceManager", result.ServiceManager)
	writePackagedInstallResultLine(&builder, "currentVersion", result.CurrentVersion)
	writePackagedInstallResultLine(&builder, "currentTrack", result.CurrentTrack)
	writePackagedInstallResultLine(&builder, "currentSlot", result.CurrentSlot)
	writePackagedInstallResultLine(&builder, "adminURL", result.AdminURL)
	writePackagedInstallResultLine(&builder, "setupURL", result.SetupURL)
	writePackagedInstallResultLine(&builder, "setupRequired", boolString(result.SetupRequired))
	writePackagedInstallResultLine(&builder, "logPath", result.LogPath)
	writePackagedInstallResultLine(&builder, "error", result.Error)

	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func writePackagedInstallResultLine(builder *strings.Builder, key, value string) {
	if builder == nil {
		return
	}
	safeValue := strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
	builder.WriteString(fmt.Sprintf("%s=%s\n", key, safeValue))
}
