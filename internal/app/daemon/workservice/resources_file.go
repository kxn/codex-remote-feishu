package workservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worklocation"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type SaveResourceFileRequest struct {
	Key              string `json:"key"`
	Path             string `json:"path"`
	Purpose          string `json:"purpose,omitempty"`
	CollectionID     string `json:"collection_id,omitempty"`
	ResourceID       string `json:"resource_id,omitempty"`
	ExpectedRevision int64  `json:"expected_revision,omitempty"`
}

type SavedResourceFile struct {
	Result
	ResourceID string `json:"resource_id"`
	VersionID  string `json:"version_id"`
}

func (s *Service) WithArtifactHost(hostID string) { s.artifactHost = hostID }

func (s *Service) ResourceSaveFile(ctx context.Context, token, runRef string, req SaveResourceFileRequest) (SavedResourceFile, error) {
	const command = "resource.file.save"
	if strings.TrimSpace(req.Key) == "" || len(req.Key) > 256 || strings.TrimSpace(req.Path) == "" || (req.ResourceID == "" && (strings.TrimSpace(req.CollectionID) == "" || strings.TrimSpace(req.Purpose) == "" || req.ExpectedRevision != 0)) || (req.ResourceID != "" && (req.ExpectedRevision < 1 || req.CollectionID != "" || req.Purpose != "")) {
		return SavedResourceFile{}, &workcmd.Error{Code: "invalid_argument", Message: "provide key and relative path; first save needs collection_id and purpose, revisions need resource_id and expected_revision"}
	}
	name, err := worklocation.DeliveryFileName(req.Path)
	if err != nil {
		return SavedResourceFile{}, err
	}
	var result SavedResourceFile
	var location work.ExecutionLocation
	var worker work.WorkerSession
	var operationID string
	started := s.now()
	replayOrSave := func(tx *workstore.Tx, c Caller, save func() (string, int64, error)) error {
		var err error
		result.Result, err = s.mutate(tx, c, command, req.Key, result.ResourceID, req, save)
		result.VersionID = result.ObjectID
		return err
	}
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		worker = c.Worker
		result.ResourceID = req.ResourceID
		if result.ResourceID == "" {
			result.ResourceID = stableID("resource-file", worker.ScopeID, worker.RoleID, req.Key)
		}
		operationID = stableID("op", worker.ScopeID, string(work.ActorRole), worker.RoleID, command, req.Key)
		if _, err := tx.GetOperation(operationID); err == nil {
			return replayOrSave(tx, c, func() (string, int64, error) { return "", 0, workstore.ErrConflict })
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if worker.LocationID == "" || s.artifactHost == "" {
			return fmt.Errorf("this run has no configured local execution directory")
		}
		var err error
		location, err = tx.GetLocation(worker.LocationID)
		if err != nil {
			return err
		}
		if location.Generation != worker.LocationGeneration || location.HostID != s.artifactHost || (location.IsolatedContextID != "" && location.IsolatedContextID != worker.ContextID) {
			return workstore.ErrScopeDenied
		}
		return nil
	})
	if err != nil || result.Replay {
		return result, err
	}
	data, err := worklocation.ReadDeliveryFile(location.Cwd, req.Path)
	if err != nil {
		return SavedResourceFile{}, fmt.Errorf("cannot save the declared file: %w", err)
	}
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		if c.Worker.LocationID != location.ID || c.Worker.LocationGeneration != location.Generation || c.Worker.CarrierIncarnation != worker.CarrierIncarnation || c.Worker.ProcessGeneration != worker.ProcessGeneration {
			return workstore.ErrConflict
		}
		current, err := tx.GetLocation(location.ID)
		if err != nil {
			return err
		}
		if current.HostID != location.HostID || current.Cwd != location.Cwd || current.Revision != location.Revision {
			return workstore.ErrConflict
		}
		return replayOrSave(tx, c, func() (string, int64, error) {
			resource := work.Resource{ID: result.ResourceID, ScopeID: c.Worker.ScopeID, CollectionID: req.CollectionID, ConnectorIdentity: work.SavedFileConnector, SourceIdentity: result.ResourceID, CanonicalURI: "relay-artifact:" + result.ResourceID, Purpose: req.Purpose, Revision: 1}
			if req.ResourceID != "" {
				resource, err = tx.GetResource(c.Worker.ScopeID, result.ResourceID)
				if err != nil {
					return "", 0, err
				}
				if resource.ConnectorIdentity != work.SavedFileConnector {
					return "", 0, fmt.Errorf("only an existing saved file can receive a file revision")
				}
				if resource.Revision != req.ExpectedRevision {
					return "", 0, workstore.ErrConflict
				}
				resource.Revision++
			}
			if err := tx.PutResource(resource, req.ExpectedRevision); err != nil {
				return "", 0, err
			}
			saved := s.now().UTC()
			body := work.NewContent(stableID("resource-file-body", operationID), resource.ScopeID, http.DetectContentType(data), data, saved)
			if err := tx.PutContent(body); err != nil {
				return "", 0, err
			}
			version := work.ResourceVersion{ID: stableID("resource-file-version", operationID), ResourceID: resource.ID, ScopeID: resource.ScopeID, Kind: work.VersionImmutableSnapshot, Source: work.SourceRef{Kind: "saved_file", ID: resource.ID, Version: strconv.FormatInt(resource.Revision, 10), EvidenceRef: body.ID}, FileName: name, ContentRef: body.ID, ContentHash: body.SHA256, Consistency: work.ConsistencyAtomic, Complete: true, Saved: true, StartedAt: started.UTC(), CapturedAt: saved}
			return version.ID, resource.Revision, tx.PutResourceVersion(version)
		})
	})
	return result, err
}
