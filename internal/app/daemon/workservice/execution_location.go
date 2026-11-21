package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worklocation"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ExecutionLocationRequest struct {
	Key              string `json:"key"`
	RootLocationID   string `json:"root_location_id"`
	Path             string `json:"path"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type ExecutionLocationView struct {
	SelectedResources []work.ExecutionLocation            `json:"selected_resources"`
	Current           work.ExecutionLocation              `json:"current"`
	Desired           *workstore.DesiredExecutionLocation `json:"desired,omitempty"`
}

func (s *Service) ExecutionLocations(ctx context.Context, token, runRef string) (ExecutionLocationView, error) {
	var result ExecutionLocationView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		preference, err := tx.GetExecutionPreference(c.Worker.ScopeID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		for _, id := range preference.SelectedLocationIDs {
			location, e := tx.GetLocation(id)
			if e != nil {
				return e
			}
			result.SelectedResources = append(result.SelectedResources, location)
		}
		if c.Worker.LocationID != "" {
			result.Current, err = tx.GetLocation(c.Worker.LocationID)
			if err != nil {
				return err
			}
		}
		desired, err := tx.GetDesiredExecutionLocation(c.Worker.ContextID)
		if err == nil {
			result.Desired = &desired
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		return nil
	})
	return result, err
}

func (s *Service) UseExecutionLocation(ctx context.Context, token, runRef string, req ExecutionLocationRequest) (Result, error) {
	var result Result
	var root work.ExecutionLocation
	var preference workstore.ExecutionPreference
	replayed := false
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		opID := stableID("op", c.Worker.ScopeID, string(work.ActorRole), c.Worker.RoleID, "execution.location.use", req.Key)
		if _, e := tx.GetOperation(opID); e == nil {
			replayed = true
			var err error
			result, err = s.mutate(tx, c, "execution.location.use", req.Key, c.Worker.ContextID, req, func() (string, int64, error) { return "", 0, fmt.Errorf("unexpected replay effect") })
			return err
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		var err error
		preference, err = tx.GetExecutionPreference(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if !slices.Contains(preference.SelectedLocationIDs, req.RootLocationID) {
			return workstore.ErrScopeDenied
		}
		root, err = tx.GetLocation(req.RootLocationID)
		return err
	})
	if err != nil || replayed {
		if err == nil {
			result.Status = "accepted_for_next_execution"
		}
		return result, err
	}
	proof, err := worklocation.Verify(ctx, root, req.Path)
	if err != nil {
		return result, err
	}
	err = s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		fresh, e := tx.GetExecutionPreference(c.Worker.ScopeID)
		if e != nil {
			return e
		}
		freshRoot, e := tx.GetLocation(root.ID)
		if e != nil {
			return e
		}
		if fresh.Revision != preference.Revision || freshRoot.Revision != root.Revision {
			return workstore.ErrConflict
		}
		var err error
		result, err = s.mutate(tx, c, "execution.location.use", req.Key, c.Worker.ContextID, req, func() (string, int64, error) {
			location := proof.Location
			old, e := tx.GetLocation(location.ID)
			if errors.Is(e, workstore.ErrNotFound) {
				if e = tx.PutLocation(location, 0); e != nil {
					return "", 0, e
				}
			} else if e != nil {
				return "", 0, e
			} else {
				location = old
				if old.HostID != proof.Location.HostID || old.Cwd != proof.Location.Cwd {
					return "", 0, workstore.ErrConflict
				}
				if !reflect.DeepEqual(old.ResourceKeys, proof.Location.ResourceKeys) {
					location.ResourceKeys = proof.Location.ResourceKeys
					location.Generation++
					location.Revision++
					if e = tx.PutLocation(location, old.Revision); e != nil {
						return "", 0, e
					}
				}
			}
			proof.Location = location
			raw, e := json.Marshal(proof)
			if e != nil {
				return "", 0, e
			}
			body := work.NewContent(stableID("location_verification", c.Worker.ContextID, req.Key), c.Worker.ScopeID, "application/json", raw, s.now())
			if e = tx.PutContent(body); e != nil {
				return "", 0, e
			}
			desired := workstore.DesiredExecutionLocation{ContextID: c.Worker.ContextID, ScopeID: c.Worker.ScopeID, LocationID: location.ID, LocationGeneration: location.Generation, RootLocationID: root.ID, AuthorizingAttemptID: c.Attempt.ID, RunRef: c.Attempt.RunRef, VerificationRef: body.ID, Revision: req.ExpectedRevision + 1}
			if e = tx.PutDesiredExecutionLocation(desired, req.ExpectedRevision); e != nil {
				return "", 0, e
			}
			continuationID := stableID("location_continuation", c.Worker.ContextID, req.Key)
			continuationBody := work.NewContent(continuationID+"_body", c.Worker.ScopeID, "text/plain", []byte("你登记的工作目录将在上一轮原生执行与后台工作安全结束后采用。请核对当前目录与检查点，继续尚未完成的工作；这不是新的用户需求，也不代表原任务已完成。"), s.now())
			if e = tx.PutContent(continuationBody); e != nil {
				return "", 0, e
			}
			message := work.Message{ID: continuationID, ScopeID: c.Worker.ScopeID, ConversationID: c.Worker.ContextID, Source: work.Actor{Kind: work.ActorSystem, ID: "execution-location"}, BodyRef: continuationBody.ID, Revision: 1, CreatedAt: s.now()}
			input := work.Inbox{ID: continuationID + "_input", ScopeID: c.Worker.ScopeID, TaskID: c.Attempt.TaskID, RecipientRoleID: c.Worker.RoleID, RecipientContextID: c.Worker.ContextID, EventID: continuationID, MessageID: message.ID, State: work.InboxPending, ContentState: work.InputContentReady, Revision: 1, AvailableAt: s.now()}
			if _, _, e = tx.AcceptMessage(message, []work.Inbox{input}); e != nil {
				return "", 0, e
			}
			return desired.LocationID, desired.Revision, nil
		})
		return err
	})
	if err == nil {
		result.Status = "accepted_for_next_execution"
	}
	return result, err
}
