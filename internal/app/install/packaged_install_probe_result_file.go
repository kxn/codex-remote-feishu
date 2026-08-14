package install

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/atomicfile"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func writePackagedInstallProbeResultFile(path string, result PackagedInstallProbeResult) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	var builder strings.Builder
	builder.WriteString("[" + packagedInstallResultSection + "]\n")
	writePackagedInstallProbeResultLine(&builder, "ok", xutil.BoolString(result.OK))
	writePackagedInstallProbeResultLine(&builder, "mode", result.Mode)
	writePackagedInstallProbeResultLine(&builder, "statePath", result.StatePath)
	writePackagedInstallProbeResultLine(&builder, "configPath", result.ConfigPath)
	writePackagedInstallProbeResultLine(&builder, "currentVersion", result.CurrentVersion)
	writePackagedInstallProbeResultLine(&builder, "currentTrack", result.CurrentTrack)
	writePackagedInstallProbeResultLine(&builder, "installerVersion", result.InstallerVersion)
	writePackagedInstallProbeResultLine(&builder, "sameVersion", xutil.BoolString(result.SameVersion))
	writePackagedInstallProbeResultLine(&builder, "currentInstallBinDir", result.CurrentInstallBinDir)
	writePackagedInstallProbeResultLine(&builder, "suggestedInstallBinDir", result.SuggestedInstallBinDir)
	writePackagedInstallProbeResultLine(&builder, "installLocationEditable", xutil.BoolString(result.InstallLocationEditable))
	writePackagedInstallProbeResultLine(&builder, "serviceManager", result.ServiceManager)
	writePackagedInstallProbeResultLine(&builder, "startupMode", result.StartupMode)
	writePackagedInstallProbeResultLine(&builder, "error", result.Error)

	return atomicfile.Write(path, []byte(builder.String()), 0o644)
}

func writePackagedInstallProbeResultLine(builder *strings.Builder, key, value string) {
	if builder == nil {
		return
	}
	safeValue := strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
	builder.WriteString(fmt.Sprintf("%s=%s\n", key, safeValue))
}
