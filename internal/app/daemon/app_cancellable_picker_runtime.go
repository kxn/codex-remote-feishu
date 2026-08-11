package daemon

import "strings"

type cancellablePickerRuntime struct {
	cancelled bool
	cancel    func()
}

func pickerRuntimeKey(surfaceSessionID, pickerID string) string {
	return strings.TrimSpace(surfaceSessionID) + "::" + strings.TrimSpace(pickerID)
}

func beginCancellablePickerRuntime(runtimes map[string]*cancellablePickerRuntime, surfaceSessionID, pickerID string, cancel func()) string {
	key := pickerRuntimeKey(surfaceSessionID, pickerID)
	if key == "::" {
		return ""
	}
	runtimes[key] = &cancellablePickerRuntime{cancel: cancel}
	return key
}

func finishCancellablePickerRuntime(runtimes map[string]*cancellablePickerRuntime, key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	runtime := runtimes[key]
	delete(runtimes, key)
	if runtime == nil {
		return false
	}
	return runtime.cancelled
}

func cancelCancellablePickerRuntime(runtimes map[string]*cancellablePickerRuntime, surfaceSessionID, pickerID string) {
	key := pickerRuntimeKey(surfaceSessionID, pickerID)
	runtime := runtimes[key]
	if runtime == nil {
		return
	}
	runtime.cancelled = true
	if runtime.cancel != nil {
		runtime.cancel()
	}
}
