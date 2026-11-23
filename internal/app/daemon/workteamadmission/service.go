// Package workteamadmission dispatches precisely targeted internal messages to
// an already occupied project turn. Delivery remains governed by native receipts.
package workteamadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type Driver interface {
	SendCommand(context.Context, string, agentproto.Command) error
}
type Service struct {
	recoveryMu sync.Mutex
	store      *workstore.Store
	service    *workservice.Service
	driver     Driver
	stateDir   string
}

func New(store *workstore.Store, service *workservice.Service, driver Driver, stateDir string) (*Service, error) {
	if store == nil || service == nil || driver == nil || !filepath.IsAbs(stateDir) {
		return nil, fmt.Errorf("project admission requires store, service, driver and absolute state root")
	}
	return &Service{store: store, service: service, driver: driver, stateDir: stateDir}, nil
}

func (s *Service) Tick(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("project admission limit must be 1..100")
	}
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()

	var candidates []workstore.ProjectSteerCandidate
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		targets, e := tx.ListSchedulingTargets()
		if e != nil {
			return e
		}
		seenContexts := map[string]bool{}
		for _, target := range targets {
			inputs, e := tx.ListProjectPendingInArrivalOrder(target.ScopeID, target.RoleID)
			if e != nil {
				return e
			}
			for _, input := range inputs {
				candidate, e := tx.ProjectSteerCandidateFor(input.ID, time.Now().UTC())
				if candidate.ContextID != "" {
					key := target.ScopeID + "\x00" + candidate.ContextID
					if seenContexts[key] {
						continue
					}
					seenContexts[key] = true
				}
				if errors.Is(e, workstore.ErrNotFound) || errors.Is(e, workstore.ErrScopeDenied) || errors.Is(e, workstore.ErrConflict) {
					continue
				}
				if e != nil {
					return e
				}
				candidates = append(candidates, candidate)
				if len(candidates) == limit {
					return nil
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, candidate := range candidates {
		if e := s.dispatch(ctx, candidate); e != nil && !errors.Is(e, workstore.ErrConflict) && !errors.Is(e, workstore.ErrScopeDenied) && !errors.Is(e, workstore.ErrNotFound) {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}
func (s *Service) dispatch(ctx context.Context, c workstore.ProjectSteerCandidate) error {
	parts, err := s.service.BuildProjectSteerWorkPrompt(ctx, c.Attempt.ID, c.Input.ID, filepath.Join(s.stateDir, "team-attachments"))
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", c.Attempt.ID, c.Input.ID, c.Input.Revision)))
	id := "project-input-admission:" + hex.EncodeToString(digest[:])
	binding := c.Turn.Binding
	command := agentproto.Command{CommandID: id + ":command", Kind: agentproto.CommandTurnSteer, Execution: &binding, InstructionRevision: c.Original.InstructionRevision, Target: agentproto.Target{ThreadID: c.Turn.SessionID, TurnID: c.Turn.TurnID}, Prompt: agentproto.Prompt{Inputs: parts}, NativeInputAdmission: &agentproto.NativeInputAdmissionCommand{Version: agentproto.NativeInputAdmissionProtocolVersion, ID: id, InputIDs: []string{c.Input.ID}}, IssuedAt: time.Now().UTC()}
	var created bool
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		_, added, e := tx.BeginAttemptInputAdmission(workstore.BeginAttemptInputAdmissionRequest{ID: id, AttemptID: c.Attempt.ID, RunRef: c.Attempt.RunRef, ExpectedWorkerRevision: c.Worker.Revision, InputIDs: []string{c.Input.ID}, Command: command, ProjectSource: &c.Source, Now: time.Now().UTC()})
		created = added
		return e
	})
	if err != nil || !created {
		return err
	}
	if err = s.driver.SendCommand(ctx, c.Worker.InstanceID, command); err != nil {
		var unsent interface{ NativeCommandUnsent() bool }
		provedUnsent := errors.As(err, &unsent) && unsent.NativeCommandUnsent()
		save := s.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			if provedUnsent {
				return tx.MarkProjectAdmissionNotSent(command.CommandID, err.Error())
			}
			return tx.MarkAttemptInputAdmissionUnknown(command.CommandID, err.Error())
		})
		if save != nil {
			return errors.Join(err, save)
		}
	}
	return nil
}
