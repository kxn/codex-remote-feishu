package workhost

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
)

func prepareMemberWorkspace(stateDir, projectID, roleID string) (string, error) {
	stateDir, err := workservice.ResolveMemberWorkspaceRoot(stateDir)
	if err != nil {
		return "", err
	}
	path, err := workservice.MemberWorkspacePath(stateDir, projectID, roleID)
	if err != nil {
		return "", err
	}
	current := stateDir
	rel, err := filepath.Rel(stateDir, path)
	if err != nil {
		return "", err
	}
	for _, part := range splitWorkspacePath(rel) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("member workspace component is not a directory: %s", current)
		}
	}
	return path, nil
}
func splitWorkspacePath(path string) []string {
	var result []string
	for path != "." && path != "" {
		dir, base := filepath.Split(path)
		result = append([]string{base}, result...)
		path = filepath.Clean(dir)
	}
	return result
}
