package editor

import (
	"encoding/json"
	"os"
)

func ClearVSCodeSettingsExecutable(settingsPath string) error {
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
	delete(settings, "chatgpt.cliExecutable")

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(settingsPath, encoded, 0o644)
}
