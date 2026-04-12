package install

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
)

const (
	repoRootEnvVar           = "CODEX_REMOTE_REPO_ROOT"
	repoLocalStateDirName    = ".codex-remote"
	repoLocalInstanceFile    = "install-instance"
	repoLocalExcludeLine     = "/.codex-remote/"
	projectModuleDeclaration = "module github.com/kxn/codex-remote-feishu"
)

type installInstanceSelection struct {
	InstanceID   string
	RepoRoot     string
	WriteBinding bool
	ClearBinding bool
}

func resolveInstallInstanceSelection(explicitValue, baseDir string) (installInstanceSelection, error) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return installInstanceSelection{}, err
	}

	trimmedExplicit := strings.TrimSpace(explicitValue)
	if trimmedExplicit != "" {
		instanceID, err := parseInstanceID(trimmedExplicit)
		if err != nil {
			return installInstanceSelection{}, err
		}
		return installInstanceSelection{
			InstanceID:   instanceID,
			RepoRoot:     repoRoot,
			WriteBinding: repoRoot != "" && !isDefaultInstance(instanceID),
			ClearBinding: repoRoot != "" && isDefaultInstance(instanceID),
		}, nil
	}

	if repoRoot == "" {
		return installInstanceSelection{InstanceID: defaultInstanceID}, nil
	}

	if instanceID, ok, err := readRepoInstallInstance(repoRoot); err != nil {
		return installInstanceSelection{}, err
	} else if ok {
		return installInstanceSelection{InstanceID: instanceID, RepoRoot: repoRoot}, nil
	}

	if repoShouldUseDedicatedInstance(baseDir) {
		return installInstanceSelection{
			InstanceID:   deriveRepoInstanceID(repoRoot),
			RepoRoot:     repoRoot,
			WriteBinding: true,
		}, nil
	}

	return installInstanceSelection{
		InstanceID: defaultInstanceID,
		RepoRoot:   repoRoot,
	}, nil
}

func persistInstallInstanceSelection(selection installInstanceSelection) error {
	if strings.TrimSpace(selection.RepoRoot) == "" {
		return nil
	}
	if selection.ClearBinding {
		return clearRepoInstallInstance(selection.RepoRoot)
	}
	if selection.WriteBinding {
		return writeRepoInstallInstance(selection.RepoRoot, selection.InstanceID)
	}
	return nil
}

func repoShouldUseDedicatedInstance(baseDir string) bool {
	return defaultInstanceInstallExists(baseDir) || !portSetAvailable(instanceDefaultPorts(defaultInstanceID))
}

func defaultInstanceInstallExists(baseDir string) bool {
	if strings.TrimSpace(baseDir) == "" {
		return false
	}
	for _, path := range []string{
		defaultInstallStatePathForInstance(baseDir, defaultInstanceID),
		defaultConfigPathForInstance(baseDir, defaultInstanceID),
	} {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func resolveRepoRoot() (string, error) {
	if repoRoot, err := normalizedRepoRootFromEnv(); err != nil || repoRoot != "" {
		return repoRoot, err
	}
	for _, start := range repoRootSearchStarts() {
		repoRoot, ok, err := findProjectRepoRoot(start)
		if err != nil {
			return "", err
		}
		if ok {
			return repoRoot, nil
		}
	}
	return "", nil
}

func normalizedRepoRootFromEnv() (string, error) {
	repoRoot := strings.TrimSpace(os.Getenv(repoRootEnvVar))
	if repoRoot == "" {
		return "", nil
	}
	repoRoot = filepath.Clean(repoRoot)
	info, err := os.Stat(repoRoot)
	if err != nil {
		return "", fmt.Errorf("stat repo root %q: %w", repoRoot, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo root %q is not a directory", repoRoot)
	}
	return repoRoot, nil
}

func repoRootSearchStarts() []string {
	var values []string
	seen := map[string]bool{}
	appendValue := func(value string) {
		value = filepath.Clean(strings.TrimSpace(value))
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		values = append(values, value)
	}
	if cwd, err := os.Getwd(); err == nil {
		appendValue(cwd)
	}
	if exe, err := executablePath(); err == nil {
		appendValue(filepath.Dir(exe))
	}
	return values
}

func findProjectRepoRoot(start string) (string, bool, error) {
	current := filepath.Clean(strings.TrimSpace(start))
	if current == "" {
		return "", false, nil
	}
	for {
		ok, err := isProjectRepoRoot(current)
		if err != nil {
			return "", false, err
		}
		if ok {
			return current, true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func isProjectRepoRoot(path string) (bool, error) {
	if info, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	} else if info == nil {
		return false, nil
	}
	raw, err := os.ReadFile(filepath.Join(path, "go.mod"))
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		return false, nil
	}
	return strings.Contains(string(raw), projectModuleDeclaration), nil
}

func repoInstallInstancePath(repoRoot string) string {
	return filepath.Join(strings.TrimSpace(repoRoot), repoLocalStateDirName, repoLocalInstanceFile)
}

func readRepoInstallInstance(repoRoot string) (string, bool, error) {
	raw, err := os.ReadFile(repoInstallInstancePath(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	instanceID, err := parseInstanceID(string(raw))
	if err != nil {
		return "", false, fmt.Errorf("invalid repo-local install instance: %w", err)
	}
	return instanceID, true, nil
}

func writeRepoInstallInstance(repoRoot, instanceID string) error {
	instanceID, err := parseInstanceID(instanceID)
	if err != nil {
		return err
	}
	path := repoInstallInstancePath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(instanceID+"\n"), 0o600); err != nil {
		return err
	}
	return ensureRepoLocalGitExclude(repoRoot)
}

func clearRepoInstallInstance(repoRoot string) error {
	path := repoInstallInstancePath(repoRoot)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func deriveRepoInstanceID(repoRoot string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(filepath.Clean(strings.TrimSpace(repoRoot))))
	return fmt.Sprintf("repo-%08x", hash.Sum32())
}

func ensureRepoLocalGitExclude(repoRoot string) error {
	gitDir, err := gitDirForRepoRoot(repoRoot)
	if err != nil || strings.TrimSpace(gitDir) == "" {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	if fileHasExactLine(excludePath, repoLocalExcludeLine) {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(repoLocalExcludeLine + "\n")
	return err
}

func gitDirForRepoRoot(repoRoot string) (string, error) {
	gitPath := filepath.Join(strings.TrimSpace(repoRoot), ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.IsDir() {
		return gitPath, nil
	}
	return parseGitDirFile(gitPath)
}

func parseGitDirFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", nil
	}
	return filepath.Clean(strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))), nil
}

func fileHasExactLine(path, expected string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}
