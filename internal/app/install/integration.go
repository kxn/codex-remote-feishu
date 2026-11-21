package install

import (
	"fmt"
	"strings"
)

type WrapperIntegrationMode string

// IntegrationManagedShim identifies existing install records for cleanup.
const IntegrationManagedShim WrapperIntegrationMode = "managed_shim"

func validateInstallIntegration(raw string) error {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto", "none":
		return nil
	default:
		return fmt.Errorf("-integration %q: VS Code session takeover integration has been retired", raw)
	}
}
