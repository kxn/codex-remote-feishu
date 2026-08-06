package install

import "fmt"

// Editor integration hooks.
//
// install deliberately does not import internal/adapter/editor: editor pulls in
// internal/managedshim/embed (the embedded vscode shim asset), and the upgrade
// shim links this package for its upgrade engine. Keeping editor out of the
// package's import graph keeps embedded shim assets out of the upgrade shim
// binary. The real editor implementations are registered once by the main
// process (launcher.Main) via RegisterEditorHooks; the upgrade shim never
// registers them and never reaches these code paths.

// BundleEntrypointPatchOptions mirrors the editor patch operation inputs.
type BundleEntrypointPatchOptions struct {
	EntrypointPath   string
	InstallStatePath string
	ConfigPath       string
	InstanceID       string
}

var (
	detectBundleEntrypointsFunc  = func(goos, goarch, homeDir string) []string { return nil }
	patchBundleEntrypointFunc    = func(BundleEntrypointPatchOptions) error { return nil }
	prepareUpgradeHelperShimFunc = func(statePath, instanceID string) (string, error) {
		return "", fmt.Errorf("upgrade shim release is not registered")
	}
)

// RegisterEditorHooks installs the editor integration implementations used by
// DetectPlatformDefaults and Service.Bootstrap. It must be called once during
// main-process startup before any install flow runs. Pass nil to keep a hook
// unchanged.
func RegisterEditorHooks(
	detect func(goos, goarch, homeDir string) []string,
	patch func(opts BundleEntrypointPatchOptions) error,
) {
	if detect != nil {
		detectBundleEntrypointsFunc = detect
	}
	if patch != nil {
		patchBundleEntrypointFunc = patch
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
