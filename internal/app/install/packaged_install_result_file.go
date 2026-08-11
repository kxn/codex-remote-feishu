package install

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const packagedInstallResultSection = "result"

func writePackagedInstallResultFile(path string, result PackagedInstallResult) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	var builder strings.Builder
	builder.WriteString("[" + packagedInstallResultSection + "]\n")
	writePackagedInstallResultLine(&builder, "ok", xutil.BoolString(result.OK))
	writePackagedInstallResultLine(&builder, "mode", result.Mode)
	writePackagedInstallResultLine(&builder, "statePath", result.StatePath)
	writePackagedInstallResultLine(&builder, "configPath", result.ConfigPath)
	writePackagedInstallResultLine(&builder, "installedBinary", result.InstalledBinary)
	writePackagedInstallResultLine(&builder, "serviceManager", result.ServiceManager)
	writePackagedInstallResultLine(&builder, "startupMode", result.StartupMode)
	writePackagedInstallResultLine(&builder, "currentVersion", result.CurrentVersion)
	writePackagedInstallResultLine(&builder, "currentTrack", result.CurrentTrack)
	writePackagedInstallResultLine(&builder, "currentSlot", result.CurrentSlot)
	writePackagedInstallResultLine(&builder, "adminURL", result.AdminURL)
	writePackagedInstallResultLine(&builder, "setupURL", result.SetupURL)
	writePackagedInstallResultLine(&builder, "setupRequired", xutil.BoolString(result.SetupRequired))
	writePackagedInstallResultLine(&builder, "logPath", result.LogPath)
	writePackagedInstallResultLine(&builder, "error", result.Error)

	return atomicfile.Write(path, []byte(builder.String()), 0o644)
}

func writePackagedInstallResultLine(builder *strings.Builder, key, value string) {
	if builder == nil {
		return
	}
	safeValue := strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
	builder.WriteString(fmt.Sprintf("%s=%s\n", key, safeValue))
}
