package daemon

import (
	"os"
	"path/filepath"
)

// Windows hardlink creation is no-replace. The caller verifies the parent
// identity before and after this path-based operation.
func privateStageLink(_ *os.File, path, oldName, newName string) error {
	return os.Link(filepath.Join(path, oldName), filepath.Join(path, newName))
}
