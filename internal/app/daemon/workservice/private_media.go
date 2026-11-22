package workservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worklocation"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PublishPrivateMediaRequest struct {
	Key       string `json:"key"`
	Path      string `json:"path,omitempty"`
	VersionID string `json:"version_id,omitempty"`
	Kind      string `json:"kind"`
}

type PrivateMediaPublication struct {
	VersionID  string `json:"version_id,omitempty"`
	FileName   string `json:"file_name"`
	Kind       string `json:"kind"`
	ContentRef string `json:"content_ref"`
	SHA256     string `json:"sha256"`
}

func (s *Service) PublishPrivateMedia(ctx context.Context, token, runRef string, req PublishPrivateMediaRequest) (Result, error) {
	if (req.Path == "") == (req.VersionID == "") {
		return Result{}, publicationArgument("Choose one relative path or saved file version.")
	}
	if req.Kind != "file" && req.Kind != "image" && req.Kind != "video" {
		return Result{}, publicationArgument("Media kind must be file, image, or video.")
	}
	var result Result
	var location work.ExecutionLocation
	var source Caller
	var name string
	var data []byte
	var err error
	if req.Path != "" {
		name, err = worklocation.DeliveryFileName(req.Path)
		if err != nil {
			return result, err
		}
		err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
			if _, err := s.authorizePublication(tx, c, "", "", "", publicationMedia, nil); err != nil {
				return err
			}
			publishID := mediaPublicationID(c, req.Key)
			if _, err := tx.GetOperation(mediaCommandID(c, req.Key)); err == nil {
				return s.replayMedia(tx, c, publishID, req, &result)
			} else if !errors.Is(err, workstore.ErrNotFound) {
				return err
			}
			source = c
			location, err = s.privateMediaLocation(tx, c)
			return err
		})
		if err != nil || result.Replay {
			return result, err
		}
		data, err = worklocation.ReadDeliveryFile(location.Cwd, req.Path)
		if err != nil {
			return result, fmt.Errorf("cannot read the selected file: %w", err)
		}
	}
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		projection, err := s.authorizePublication(tx, c, "", "", "", publicationMedia, nil)
		if err != nil {
			return err
		}
		id := mediaPublicationID(c, req.Key)
		result, err = s.mutate(tx, c, "media.publish", req.Key, id, req, func() (string, int64, error) {
			media := PrivateMediaPublication{Kind: req.Kind, VersionID: req.VersionID, FileName: name}
			var content work.Content
			if req.VersionID != "" {
				version, err := tx.GetResourceVersion(c.Worker.ScopeID, req.VersionID)
				if err != nil {
					return "", 0, publicationRecordError(err)
				}
				media.FileName, media.ContentRef, media.SHA256 = version.FileName, version.ContentRef, version.ContentHash
				content, err = PrivateMediaContent(tx, c.Worker.ScopeID, media)
				if err != nil {
					return "", 0, err
				}
			} else {
				current, err := s.privateMediaLocation(tx, c)
				if err != nil {
					return "", 0, err
				}
				if current.ID != location.ID || current.Cwd != location.Cwd || current.Revision != location.Revision || c.Attempt.ID != source.Attempt.ID || c.Worker.ProcessGeneration != source.Worker.ProcessGeneration {
					return "", 0, workstore.ErrConflict
				}
				content = work.NewContent(stableID("media-body", id), c.Worker.ScopeID, http.DetectContentType(data), data, s.now())
				media.ContentRef, media.SHA256 = content.ID, content.SHA256
				if err = tx.PutContent(content); err != nil {
					return "", 0, err
				}
			}
			if (media.Kind == "image" && !strings.HasPrefix(content.MediaType, "image/")) ||
				(media.Kind == "video" && !strings.HasPrefix(content.MediaType, "video/")) {
				return "", 0, publicationArgument("The selected file does not match the media kind.")
			}
			if media.Kind == "image" && len(content.Data) > feishu.MaxPrivateImageBytes {
				return "", 0, publicationArgument("Image must be no larger than 10 MB.")
			}
			projection.Media = &media
			projection.Purpose = PublicationReply
			return id, 1, s.queueRolePublication(tx, c, id, projection)
		})
		return err
	})
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}

func mediaPublicationID(c Caller, key string) string {
	return stableID("private-media", c.Worker.ScopeID, c.Worker.RoleID, key)
}

func mediaCommandID(c Caller, key string) string {
	return stableID("op", c.Worker.ScopeID, string(work.ActorRole), c.Worker.RoleID, "media.publish", key)
}

func (s *Service) replayMedia(tx *workstore.Tx, c Caller, id string, req PublishPrivateMediaRequest, result *Result) error {
	var err error
	*result, err = s.mutate(tx, c, "media.publish", req.Key, id, req, func() (string, int64, error) { return "", 0, workstore.ErrConflict })
	if err == nil {
		result.Status = "accepted"
	}
	return err
}

func (s *Service) privateMediaLocation(tx *workstore.Tx, c Caller) (work.ExecutionLocation, error) {
	var location work.ExecutionLocation
	if c.Worker.LocationID == "" || s.artifactHost == "" {
		return location, ErrPublicationDenied
	}
	location, err := tx.GetLocation(c.Worker.LocationID)
	if err != nil {
		return location, publicationRecordError(err)
	}
	if location.HostID != s.artifactHost || location.Generation != c.Worker.LocationGeneration ||
		(location.IsolatedContextID != "" && location.IsolatedContextID != c.Worker.ContextID) {
		return location, ErrPublicationDenied
	}
	return location, nil
}

// PrivateMediaContent loads the exact bytes authorized by a media publication.
func PrivateMediaContent(tx *workstore.Tx, scopeID string, media PrivateMediaPublication) (work.Content, error) {
	var content work.Content
	if media.VersionID != "" {
		version, err := tx.GetResourceVersion(scopeID, media.VersionID)
		if err != nil {
			return content, publicationRecordError(err)
		}
		resource, err := tx.GetResource(scopeID, version.ResourceID)
		if err != nil {
			return content, publicationRecordError(err)
		}
		if resource.ConnectorIdentity != work.SavedFileConnector || !version.Saved || !version.Complete ||
			version.FileName != media.FileName || version.ContentRef != media.ContentRef || version.ContentHash != media.SHA256 {
			return content, ErrPublicationDenied
		}
	}
	content, err := tx.GetContent(scopeID, media.ContentRef)
	if err != nil {
		return content, publicationRecordError(err)
	}
	if content.SHA256 != media.SHA256 || len(content.Data) == 0 || len(content.Data) > 30_000_000 {
		return content, ErrPublicationDenied
	}
	return content, nil
}
