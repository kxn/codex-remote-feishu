package workspaceimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ImportErrorCode string

const (
	ImportErrorGitMissing           ImportErrorCode = "git_import_git_missing"
	ImportErrorInvalidURL           ImportErrorCode = "git_import_invalid_url"
	ImportErrorInvalidDirectoryName ImportErrorCode = "git_import_invalid_directory_name"
	ImportErrorDestinationExists    ImportErrorCode = "git_import_destination_exists"
	ImportErrorCloneFailed          ImportErrorCode = "git_import_clone_failed"
	ImportErrorRefNotFound          ImportErrorCode = "git_import_ref_not_found"
	ImportErrorAuthFailed           ImportErrorCode = "git_import_auth_failed"
)

type ImportRequest struct {
	RepoURL       string
	RefName       string
	ParentDir     string
	DirectoryName string
}

type PreviewResult struct {
	ParentDir           string
	DirectoryName       string
	DestinationPath     string
	ParentDirHasEntries bool
}

type ImportError struct {
	Code            ImportErrorCode
	Message         string
	RepoURL         string
	RefName         string
	ParentDir       string
	DestinationPath string
	Stderr          string
	Err             error
}

func (e *ImportError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *ImportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Preview(req ImportRequest) (PreviewResult, error) {
	repoURL := strings.TrimSpace(req.RepoURL)
	if repoURL == "" {
		return PreviewResult{}, &ImportError{Code: ImportErrorInvalidURL, Message: "git repo url is required"}
	}
	parentDir, err := resolveParentDir(req.ParentDir)
	if err != nil {
		return PreviewResult{}, err
	}
	directoryName, err := resolveDirectoryName(repoURL, req.DirectoryName)
	if err != nil {
		return PreviewResult{}, err
	}
	destinationPath := filepath.Join(parentDir, directoryName)
	if _, statErr := os.Stat(destinationPath); statErr == nil {
		return PreviewResult{}, &ImportError{
			Code:            ImportErrorDestinationExists,
			Message:         "destination already exists",
			RepoURL:         repoURL,
			ParentDir:       parentDir,
			DestinationPath: destinationPath,
		}
	} else if !os.IsNotExist(statErr) {
		return PreviewResult{}, &ImportError{
			Code:            ImportErrorCloneFailed,
			Message:         "failed to inspect destination",
			RepoURL:         repoURL,
			ParentDir:       parentDir,
			DestinationPath: destinationPath,
			Err:             statErr,
		}
	}
	dirEntries, readErr := os.ReadDir(parentDir)
	if readErr != nil {
		return PreviewResult{}, &ImportError{
			Code:      ImportErrorCloneFailed,
			Message:   "failed to inspect parent directory",
			RepoURL:   repoURL,
			ParentDir: parentDir,
			Err:       readErr,
		}
	}
	return PreviewResult{
		ParentDir:           parentDir,
		DirectoryName:       directoryName,
		DestinationPath:     destinationPath,
		ParentDirHasEntries: len(dirEntries) != 0,
	}, nil
}

func resolveParentDir(parentDir string) (string, error) {
	parentDir = strings.TrimSpace(parentDir)
	if parentDir == "" {
		return "", &ImportError{Code: ImportErrorCloneFailed, Message: "parent directory is required"}
	}
	parentDir = filepath.Clean(parentDir)
	info, err := os.Stat(parentDir)
	switch {
	case os.IsNotExist(err):
		return "", &ImportError{Code: ImportErrorCloneFailed, Message: "parent directory does not exist", ParentDir: parentDir, Err: err}
	case err != nil:
		return "", &ImportError{Code: ImportErrorCloneFailed, Message: "failed to access parent directory", ParentDir: parentDir, Err: err}
	case !info.IsDir():
		return "", &ImportError{Code: ImportErrorCloneFailed, Message: "parent path is not a directory", ParentDir: parentDir}
	default:
		return parentDir, nil
	}
}

func resolveDirectoryName(repoURL, directoryName string) (string, error) {
	if strings.TrimSpace(directoryName) != "" {
		if err := validateDirectoryName(directoryName); err != nil {
			return "", &ImportError{Code: ImportErrorInvalidDirectoryName, Message: err.Error(), RepoURL: strings.TrimSpace(repoURL)}
		}
		return strings.TrimSpace(directoryName), nil
	}
	inferred := inferDirectoryName(repoURL)
	if inferred == "" {
		return "", &ImportError{Code: ImportErrorInvalidURL, Message: "failed to infer directory name from repo url", RepoURL: strings.TrimSpace(repoURL)}
	}
	return inferred, nil
}

func validateDirectoryName(directoryName string) error {
	name := strings.TrimSpace(directoryName)
	switch name {
	case "", ".", "..":
		return fmt.Errorf("directory name is invalid")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("directory name must not contain path separators")
	}
	if strings.TrimSpace(sanitizeDirectoryName(name)) != name {
		return fmt.Errorf("directory name contains unsupported characters")
	}
	return nil
}

func inferDirectoryName(repoURL string) string {
	value := strings.TrimSpace(repoURL)
	value = strings.TrimSuffix(value, "/")
	if value == "" {
		return ""
	}
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	separator := lastSlash
	if lastColon > separator {
		separator = lastColon
	}
	if separator >= 0 {
		value = value[separator+1:]
	}
	value = strings.TrimSuffix(value, ".git")
	return sanitizeDirectoryName(value)
}

func sanitizeDirectoryName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".git")
	replacer := strings.NewReplacer(
		"<", "-",
		">", "-",
		":", "-",
		"\"", "-",
		"/", "-",
		"\\", "-",
		"|", "-",
		"?", "-",
		"*", "-",
	)
	value = replacer.Replace(value)
	value = strings.Trim(value, ". ")
	value = strings.TrimSpace(value)
	return value
}
