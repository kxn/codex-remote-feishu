package editor

import (
	"encoding/json"
	"os"
	"strings"
)

func ClearVSCodeSettingsExecutable(settingsPath string) error {
	return SetVSCodeSettingsExecutable(settingsPath, "")
}

func SetVSCodeSettingsExecutable(settingsPath, executable string) error {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	settings, err := decodeVSCodeSettings(raw)
	if err != nil {
		return err
	}
	executable = strings.TrimSpace(executable)
	if executable == "" {
		delete(settings, "chatgpt.cliExecutable")
	} else {
		settings["chatgpt.cliExecutable"] = executable
	}

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(settingsPath, encoded, 0o644)
}
