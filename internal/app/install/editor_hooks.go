package install

import "fmt"

// Editor integration hooks.
//
// install deliberately does not import internal/adapter/editor: editor pulls in
// internal/shim/embed (the embedded unified shim asset), and the upgrade
// shim links this package for its upgrade engine. Keeping editor out of the
// package's import graph keeps embedded shim assets out of the upgrade shim
// binary. The real editor implementations are registered once by the main
// process (launcher.Main) via RegisterEditorHooks; the upgrade shim never
// registers them and never reaches these code paths.

var (
	detectBundleEntrypointsFunc  = func(goos, goarch, homeDir string) []string { return nil }
	prepareUpgradeHelperShimFunc = func(statePath, instanceID string) (string, error) {
		return "", fmt.Errorf("upgrade shim release is not registered")
	}
)

// RegisterEditorHooks supplies read-only detection for legacy cleanup.
func RegisterEditorHooks(detect func(goos, goarch, homeDir string) []string) {
	if detect != nil {
		detectBundleEntrypointsFunc = detect
	}
}

// RegisterUpgradeHelperShimHook installs the embedded upgrade shim release
// implementation used by the local-upgrade flow. The upgrade shim binary itself
// never registers this and never reaches this code path.
func RegisterUpgradeHelperShimHook(release func(statePath, instanceID string) (string, error)) {
	if release != nil {
		prepareUpgradeHelperShimFunc = release
	}
}
