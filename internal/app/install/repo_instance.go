package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
)

const (
	repoRootEnvVar           = "CODEX_REMOTE_REPO_ROOT"
	repoLocalStateDirName    = ".codex-remote"
	repoLocalInstanceFile    = "install-instance"
	repoLocalBindingFile     = "install-target.json"
	repoLocalBindingVersion  = 1
	repoLocalExcludeLine     = "/.codex-remote/"
	projectModuleDeclaration = "module github.com/kxn/codex-remote-feishu"
)

type repoInstallBinding struct {
	Version         int    `json:"version"`
	InstanceID      string `json:"instanceId"`
	BaseDir         string `json:"baseDir,omitempty"`
	InstallBinDir   string `json:"installBinDir,omitempty"`
	ConfigPath      string `json:"configPath,omitempty"`
	StatePath       string `json:"statePath,omitempty"`
	LogPath         string `json:"logPath,omitempty"`
	ServiceName     string `json:"serviceName,omitempty"`
	ServiceUnitPath string `json:"serviceUnitPath,omitempty"`
}

type repoInstallBindingSource string

const (
	repoInstallBindingSourceFile   repoInstallBindingSource = "binding_file"
	repoInstallBindingSourceLegacy repoInstallBindingSource = "legacy_binding"
)

type installInstanceSelection struct {
	InstanceID      string
	BaseDir         string
	InstallBinDir   string
	ConfigPath      string
	StatePath       string
	LogPath         string
	ServiceName     string
	ServiceUnitPath string
	RepoRoot        string
	WriteBinding    bool
	ClearBinding    bool
}

func resolveInstallInstanceSelection(explicitValue, explicitBaseDir, fallbackBaseDir, goos string) (installInstanceSelection, error) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return installInstanceSelection{}, err
	}
	binding, bindingFound, err := readRepoInstallBinding(repoRoot)
	if err != nil {
		return installInstanceSelection{}, err
	}

	trimmedExplicit := strings.TrimSpace(explicitValue)
	trimmedExplicitBaseDir := strings.TrimSpace(explicitBaseDir)
	instanceID := defaultInstanceID
	if trimmedExplicit != "" {
		instanceID, err = parseInstanceID(trimmedExplicit)
		if err != nil {
			return installInstanceSelection{}, err
		}
	} else if bindingFound {
		instanceID = binding.InstanceID
	}

	resolvedBaseDir := trimmedExplicitBaseDir
	if resolvedBaseDir == "" {
		if bindingFound && strings.TrimSpace(binding.BaseDir) != "" {
			resolvedBaseDir = binding.BaseDir
		} else {
			resolvedBaseDir = detectExistingInstanceBaseDir(repoRoot, instanceID, fallbackBaseDir)
		}
	}
	if resolvedBaseDir == "" {
		resolvedBaseDir = fallbackBaseDir
	}
	resolvedBaseDir = filepath.Clean(strings.TrimSpace(resolvedBaseDir))

	layout := installLayoutForInstance(resolvedBaseDir, instanceID)
	selection := installInstanceSelection{
		InstanceID:      instanceID,
		BaseDir:         resolvedBaseDir,
		InstallBinDir:   defaultInstallBinDirForInstance(goos, resolvedBaseDir, instanceID),
		ConfigPath:      defaultConfigPathForInstance(resolvedBaseDir, instanceID),
		StatePath:       defaultInstallStatePathForInstance(resolvedBaseDir, instanceID),
		LogPath:         filepath.Join(layout.StateDir, "logs", "codex-remote-relayd.log"),
		ServiceName:     systemdUserServiceNameForInstance(instanceID),
		ServiceUnitPath: systemdUserUnitPathForInstance(resolvedBaseDir, instanceID),
		RepoRoot:        repoRoot,
	}

	if repoRoot != "" {
		switch {
		case trimmedExplicit == "" && trimmedExplicitBaseDir == "":
		case isDefaultInstance(instanceID) && trimmedExplicitBaseDir == "":
			selection.ClearBinding = true
		default:
			selection.WriteBinding = true
		}
	}

	return selection, nil
}

func persistInstallInstanceSelection(selection installInstanceSelection) error {
	if strings.TrimSpace(selection.RepoRoot) == "" {
		return nil
	}
	if selection.ClearBinding {
		return clearRepoInstallInstance(selection.RepoRoot)
	}
	if selection.WriteBinding {
		return writeRepoInstallBinding(selection.RepoRoot, repoInstallBinding{
			Version:         repoLocalBindingVersion,
			InstanceID:      selection.InstanceID,
			BaseDir:         selection.BaseDir,
			InstallBinDir:   selection.InstallBinDir,
			ConfigPath:      selection.ConfigPath,
			StatePath:       selection.StatePath,
			LogPath:         selection.LogPath,
			ServiceName:     selection.ServiceName,
			ServiceUnitPath: selection.ServiceUnitPath,
		})
	}
	return nil
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

func repoInstallBindingPath(repoRoot string) string {
	return filepath.Join(strings.TrimSpace(repoRoot), repoLocalStateDirName, repoLocalBindingFile)
}

func readRepoInstallBinding(repoRoot string) (repoInstallBinding, bool, error) {
	binding, _, ok, err := readRepoInstallBindingWithSource(repoRoot)
	return binding, ok, err
}

func readRepoInstallBindingWithSource(repoRoot string) (repoInstallBinding, repoInstallBindingSource, bool, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return repoInstallBinding{}, "", false, nil
	}
	raw, err := os.ReadFile(repoInstallBindingPath(repoRoot))
	if err != nil {
		if os.IsNotExist(err) {
			instanceID, ok, legacyErr := readLegacyRepoInstallInstance(repoRoot)
			if legacyErr != nil {
				return repoInstallBinding{}, "", false, legacyErr
			}
			if !ok {
				return repoInstallBinding{}, "", false, nil
			}
			return repoInstallBinding{
				Version:    repoLocalBindingVersion,
				InstanceID: instanceID,
			}, repoInstallBindingSourceLegacy, true, nil
		}
		return repoInstallBinding{}, "", false, err
	}

	var binding repoInstallBinding
	if err := json.Unmarshal(raw, &binding); err != nil {
		return repoInstallBinding{}, "", false, fmt.Errorf("invalid repo-local install binding: %w", err)
	}
	if binding.Version == 0 {
		binding.Version = repoLocalBindingVersion
	}
	if binding.Version != repoLocalBindingVersion {
		return repoInstallBinding{}, "", false, fmt.Errorf("unsupported repo-local install binding version %d", binding.Version)
	}
	instanceID, err := parseInstanceID(binding.InstanceID)
	if err != nil {
		return repoInstallBinding{}, "", false, fmt.Errorf("invalid repo-local install binding instance: %w", err)
	}
	binding.InstanceID = instanceID
	binding.BaseDir = normalizeBindingBaseDir(binding.BaseDir)
	return binding, repoInstallBindingSourceFile, true, nil
}

func readRepoInstallInstance(repoRoot string) (string, bool, error) {
	binding, ok, err := readRepoInstallBinding(repoRoot)
	if err != nil || !ok {
		return "", ok, err
	}
	return binding.InstanceID, true, nil
}

func readLegacyRepoInstallInstance(repoRoot string) (string, bool, error) {
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

func writeRepoInstallBinding(repoRoot string, binding repoInstallBinding) error {
	instanceID, err := parseInstanceID(binding.InstanceID)
	if err != nil {
		return err
	}
	binding.Version = repoLocalBindingVersion
	binding.InstanceID = instanceID
	binding.BaseDir = normalizeBindingBaseDir(binding.BaseDir)

	jsonPath := repoInstallBindingPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(repoInstallInstancePath(repoRoot), []byte(instanceID+"\n"), 0o600); err != nil {
		return err
	}
	return ensureRepoLocalGitExclude(repoRoot)
}

func clearRepoInstallInstance(repoRoot string) error {
	for _, path := range []string{
		repoInstallBindingPath(repoRoot),
		repoInstallInstancePath(repoRoot),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func normalizeBindingBaseDir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func detectExistingInstanceBaseDir(repoRoot, instanceID, fallbackBaseDir string) string {
	for _, candidate := range repoInstallBaseDirCandidates(repoRoot, fallbackBaseDir) {
		if installArtifactsExistForInstance(candidate, instanceID) {
			return candidate
		}
	}
	return normalizeBindingBaseDir(fallbackBaseDir)
}

func repoInstallBaseDirCandidates(repoRoot, fallbackBaseDir string) []string {
	var values []string
	seen := map[string]bool{}
	appendValue := func(value string) {
		value = normalizeBindingBaseDir(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		values = append(values, value)
	}
	current := normalizeBindingBaseDir(repoRoot)
	for current != "" {
		appendValue(current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	appendValue(fallbackBaseDir)
	return values
}

func installArtifactsExistForInstance(baseDir, instanceID string) bool {
	for _, path := range []string{
		defaultInstallStatePathForInstance(baseDir, instanceID),
		defaultConfigPathForInstance(baseDir, instanceID),
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
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
	if gitmeta.FileHasExactTrimmedLine(excludePath, repoLocalExcludeLine) {
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
	parsed, err := gitmeta.ParseGitDirFile(gitPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed) == "" {
		return "", nil
	}
	return gitmeta.ResolveGitDirPath(repoRoot, parsed), nil
}
