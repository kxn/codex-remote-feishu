package daemon

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const defaultImageStagingCleanupHours = 24

type imageStagingStatusResponse struct {
	RootDir         string `json:"rootDir"`
	FileCount       int    `json:"fileCount"`
	TotalBytes      int64  `json:"totalBytes"`
	ActiveFileCount int    `json:"activeFileCount"`
	ActiveBytes     int64  `json:"activeBytes"`
}

type imageStagingCleanupRequest struct {
	OlderThanHours int `json:"olderThanHours,omitempty"`
}

type imageStagingCleanupResponse struct {
	RootDir            string `json:"rootDir"`
	OlderThanHours     int    `json:"olderThanHours"`
	DeletedFiles       int    `json:"deletedFiles"`
	DeletedBytes       int64  `json:"deletedBytes"`
	SkippedActiveCount int    `json:"skippedActiveCount"`
	RemainingFileCount int    `json:"remainingFileCount"`
	RemainingBytes     int64  `json:"remainingBytes"`
}

type imageStagingEntry struct {
	Path    string
	Size    int64
	ModTime time.Time
	Active  bool
}

type imageStagingScan struct {
	FileCount       int
	TotalBytes      int64
	ActiveFileCount int
	ActiveBytes     int64
	Entries         []imageStagingEntry
}

func (a *App) handleImageStagingStatus(w http.ResponseWriter, _ *http.Request) {
	rootDir := a.imageStagingRootDir()
	activeRefs, err := a.imageStagingActiveRefs()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, apiError{Code: "image_staging_owner_unavailable", Message: "Cannot safely inspect active image ownership", Details: err.Error()})
		return
	}
	scan, err := scanImageStaging(rootDir, activeRefs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "image_staging_scan_failed",
			Message: "failed to scan image staging directory",
			Details: err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, imageStagingStatusResponse{
		RootDir:         rootDir,
		FileCount:       scan.FileCount,
		TotalBytes:      scan.TotalBytes,
		ActiveFileCount: scan.ActiveFileCount,
		ActiveBytes:     scan.ActiveBytes,
	})
}

func (a *App) handleImageStagingCleanup(w http.ResponseWriter, r *http.Request) {
	req := imageStagingCleanupRequest{OlderThanHours: defaultImageStagingCleanupHours}
	if r.Body != nil && r.Body != http.NoBody {
		defer r.Body.Close()
		if body, err := io.ReadAll(r.Body); err != nil {
			writeAPIError(w, http.StatusBadRequest, apiError{
				Code:    "invalid_request",
				Message: "failed to read image staging cleanup payload",
				Details: err.Error(),
			})
			return
		} else if len(strings.TrimSpace(string(body))) > 0 {
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			if err := decodeJSONBody(r, &req); err != nil && !errors.Is(err, io.EOF) {
				writeAPIError(w, http.StatusBadRequest, apiError{
					Code:    "invalid_request",
					Message: "failed to decode image staging cleanup payload",
					Details: err.Error(),
				})
				return
			}
		}
	}
	if req.OlderThanHours <= 0 {
		req.OlderThanHours = defaultImageStagingCleanupHours
	}

	rootDir := a.imageStagingRootDir()
	activeRefs, err := a.imageStagingActiveRefs()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, apiError{Code: "image_staging_owner_unavailable", Message: "Cannot safely inspect active image ownership", Details: err.Error()})
		return
	}
	scan, err := scanImageStaging(rootDir, activeRefs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "image_staging_scan_failed",
			Message: "failed to scan image staging directory",
			Details: err.Error(),
		})
		return
	}

	cutoff := time.Now().Add(-time.Duration(req.OlderThanHours) * time.Hour)
	resp := imageStagingCleanupResponse{
		RootDir:        rootDir,
		OlderThanHours: req.OlderThanHours,
	}
	for _, entry := range scan.Entries {
		if entry.ModTime.After(cutoff) {
			continue
		}
		if entry.Active {
			resp.SkippedActiveCount++
			continue
		}
		if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
			writeAPIError(w, http.StatusInternalServerError, apiError{
				Code:    "image_staging_cleanup_failed",
				Message: "failed to remove staged image file",
				Details: err.Error(),
			})
			return
		}
		resp.DeletedFiles++
		resp.DeletedBytes += entry.Size
	}
	_ = pruneEmptyDirectories(rootDir)

	remaining, err := scanImageStaging(rootDir, activeRefs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, apiError{
			Code:    "image_staging_scan_failed",
			Message: "failed to rescan image staging directory",
			Details: err.Error(),
		})
		return
	}
	resp.RemainingFileCount = remaining.FileCount
	resp.RemainingBytes = remaining.TotalBytes
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) imageStagingRootDir() string {
	stateDir := strings.TrimSpace(a.headlessRuntime.Paths.StateDir)
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "image-staging")
}

func (a *App) imageStagingActiveRefs() (map[string]struct{}, error) {
	if a.applications != nil && a.applications.processor != nil {
		observed, err := a.applications.processor.ReadRuntimeStatus(context.Background())
		if err != nil {
			return nil, err
		}
		return imageStagingRefsForSurfaces(observed.Surfaces), nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return imageStagingRefsForSurfaces(a.service.Surfaces()), nil
}

func imageStagingRefsForSurfaces(surfaces []*state.SurfaceConsoleRecord) map[string]struct{} {
	active := map[string]struct{}{}
	for _, surfaceRecord := range surfaces {
		if surfaceRecord == nil {
			continue
		}
		for _, image := range surfaceRecord.StagedImages {
			if image == nil || image.State != state.ImageStaged {
				continue
			}
			if key := imageStagingPathKey(image.LocalPath); key != "" {
				active[key] = struct{}{}
			}
		}
		for _, item := range surfaceRecord.QueueItems {
			if item == nil {
				continue
			}
			switch item.Status {
			case state.QueueItemQueued, state.QueueItemDispatching, state.QueueItemRunning:
			default:
				continue
			}
			for _, input := range item.Inputs {
				if input.Type != agentproto.InputLocalImage {
					continue
				}
				if key := imageStagingPathKey(input.Path); key != "" {
					active[key] = struct{}{}
				}
			}
		}
	}
	return active
}

func scanImageStaging(rootDir string, activeRefs map[string]struct{}) (imageStagingScan, error) {
	var scan imageStagingScan
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return scan, nil
	}
	if _, err := os.Stat(rootDir); err != nil {
		if os.IsNotExist(err) {
			return scan, nil
		}
		return scan, err
	}
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && d.IsDir() && d.Name() == privatestaging.DirectoryName {
			return filepath.SkipDir
		}
		if d == nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := imageStagingEntry{
			Path:    path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if _, ok := activeRefs[imageStagingPathKey(path)]; ok {
			entry.Active = true
			scan.ActiveFileCount++
			scan.ActiveBytes += entry.Size
		}
		scan.FileCount++
		scan.TotalBytes += entry.Size
		scan.Entries = append(scan.Entries, entry)
		return nil
	})
	return scan, err
}

func imageStagingPathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func pruneEmptyDirectories(rootDir string) error {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil
	}
	var dirs []string
	if err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d != nil && d.IsDir() {
			if d.Name() == privatestaging.DirectoryName {
				return filepath.SkipDir
			}
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		path := dirs[i]
		if path == rootDir {
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
