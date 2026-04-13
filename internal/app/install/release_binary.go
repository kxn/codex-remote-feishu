package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type ReleaseBinaryOptions struct {
	Repository   string
	BaseURL      string
	Version      string
	VersionsRoot string
	Client       *http.Client
}

var releaseBinaryRename = os.Rename

func EnsureReleaseBinary(ctx context.Context, opts ReleaseBinaryOptions) (string, error) {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		return "", fmt.Errorf("release version is required")
	}
	versionsRoot := strings.TrimSpace(opts.VersionsRoot)
	if versionsRoot == "" {
		return "", fmt.Errorf("versions root is required")
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	targetDir := filepath.Join(versionsRoot, version)
	targetBinary := filepath.Join(targetDir, executableName(goos))
	if info, err := os.Stat(targetBinary); err == nil && info.Mode().IsRegular() {
		return targetBinary, nil
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	assetName := releaseAssetName(version, goos, goarch)
	assetURL := releaseAssetURL(opts.Repository, opts.BaseURL, version, assetName)

	tempDir, err := os.MkdirTemp("", "codex-remote-release-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	archivePath := filepath.Join(tempDir, assetName)
	if err := downloadFile(ctx, client, assetURL, archivePath); err != nil {
		return "", err
	}
	if err := extractReleaseArchive(archivePath, tempDir, goos); err != nil {
		return "", err
	}

	packageDir := filepath.Join(tempDir, releasePackageDir(version, goos, goarch))
	if info, err := os.Stat(packageDir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("downloaded archive missing package directory %s", filepath.Base(packageDir))
	}

	if err := os.MkdirAll(versionsRoot, 0o755); err != nil {
		return "", err
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return "", err
	}
	if err := moveReleasePackageDir(packageDir, targetDir); err != nil {
		return "", err
	}
	return targetBinary, nil
}

func releaseAssetName(version, goos, goarch string) string {
	return fmt.Sprintf("codex-remote-feishu_%s_%s_%s.%s", strings.TrimPrefix(version, "v"), goos, goarch, releaseArchiveExtension(goos))
}

func releasePackageDir(version, goos, goarch string) string {
	return fmt.Sprintf("codex-remote-feishu_%s_%s_%s", strings.TrimPrefix(version, "v"), goos, goarch)
}

func releaseAssetURL(repository, baseURL, version, assetName string) string {
	if trimmed := strings.TrimSpace(baseURL); trimmed != "" {
		return strings.TrimRight(trimmed, "/") + "/" + assetName
	}
	repo := strings.TrimSpace(repository)
	if repo == "" {
		repo = defaultReleaseRepository
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, version, assetName)
}

func releaseArchiveExtension(goos string) string {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return "zip"
	}
	return "tar.gz"
}

func updateCurrentReleaseLink(versionsRoot, version string) error {
	if strings.TrimSpace(versionsRoot) == "" || strings.TrimSpace(version) == "" {
		return nil
	}
	currentLink := filepath.Join(versionsRoot, "current")
	targetDir := filepath.Join(versionsRoot, version)
	_ = os.Remove(currentLink)
	return os.Symlink(targetDir, currentLink)
}

func downloadFile(ctx context.Context, client *http.Client, url, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: http %d", resp.StatusCode)
	}

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func extractTarGz(archivePath, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}
		path := filepath.Join(targetDir, name)
		if !strings.HasPrefix(path, filepath.Clean(targetDir)+string(filepath.Separator)) && path != filepath.Clean(targetDir) {
			return fmt.Errorf("archive entry escaped target dir: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

func extractReleaseArchive(archivePath, targetDir, goos string) error {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return extractZip(archivePath, targetDir)
	}
	return extractTarGz(archivePath, targetDir)
}

func extractZip(archivePath, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	cleanTarget := filepath.Clean(targetDir)
	targetPrefix := cleanTarget + string(filepath.Separator)

	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." {
			continue
		}
		path := filepath.Join(cleanTarget, name)
		if path != cleanTarget && !strings.HasPrefix(path, targetPrefix) {
			return fmt.Errorf("archive entry escaped target dir: %s", file.Name)
		}

		info := file.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(path, info.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		target, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			source.Close()
			return err
		}
		if _, err := io.Copy(target, source); err != nil {
			target.Close()
			source.Close()
			return err
		}
		if err := target.Close(); err != nil {
			source.Close()
			return err
		}
		if err := source.Close(); err != nil {
			return err
		}
	}
	return nil
}

func moveReleasePackageDir(sourceDir, targetDir string) error {
	if err := releaseBinaryRename(sourceDir, targetDir); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	return copyReleasePackageDir(sourceDir, targetDir)
}

func copyReleasePackageDir(sourceDir, targetDir string) error {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		destPath := targetDir
		if rel != "." {
			destPath = filepath.Join(targetDir, rel)
		}
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported package entry %s", path)
		}
		return copyReleaseFile(path, destPath, info.Mode().Perm())
	})
}

func copyReleaseFile(sourcePath, targetPath string, mode os.FileMode) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer targetFile.Close()
	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Chmod(mode)
}
