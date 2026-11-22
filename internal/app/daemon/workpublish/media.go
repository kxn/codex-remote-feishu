package workpublish

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivateMediaSender interface {
	UploadPrivateMedia(context.Context, string, string, []byte) (string, error)
	PublishPrivateMedia(context.Context, feishu.ProjectPublishRequest, string, string) (feishu.ProjectPublishReceipt, error)
}

func (p *Publisher) sendPrivateMedia(ctx context.Context, sender Sender, job *publication, uuid string) (feishu.ProjectPublishReceipt, error, bool) {
	var receipt feishu.ProjectPublishReceipt
	adapter, ok := sender.(PrivateMediaSender)
	if !ok {
		return receipt, fmt.Errorf("private media publication adapter is unavailable"), true
	}
	media := *job.Projection.Media
	var data []byte
	var key string
	var upload bool
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		content, err := p.validatePrivateMedia(tx, job)
		if err != nil {
			return err
		}
		data = content.Data
		for _, step := range job.Operation.Steps {
			if step.ID != "media_upload" {
				continue
			}
			switch step.State {
			case work.OperationConfirmed:
				key = step.Receipt
				return nil
			case work.OperationSending, work.OperationUnknown:
				return errMediaUploadUncertain
			}
		}
		upload = true
		return p.recordMediaUpload(tx, job, work.OperationSending, "")
	})
	if err != nil {
		return receipt, err, !errors.Is(err, errMediaUploadUncertain)
	}
	if upload {
		key, err = adapter.UploadPrivateMedia(ctx, media.Kind, media.FileName, data)
		if err == nil && key == "" {
			err = errMediaUploadUncertain
		}
		uploadState := work.OperationConfirmed
		if err != nil {
			uploadState = work.OperationUnknown
			var apiErr *feishu.APIError
			if errors.As(err, &apiErr) && apiErr.Code != 0 && apiErr.StatusCode < 500 {
				uploadState = work.OperationRetryable
			}
			key = ""
		}
		saveErr := p.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			return p.recordMediaUpload(tx, job, uploadState, key)
		})
		if err != nil || saveErr != nil {
			return receipt, errors.Join(err, saveErr), uploadState == work.OperationRetryable && saveErr == nil
		}
	}
	err = p.store.Within(ctx, func(tx *workstore.Tx) error {
		_, err := p.validatePrivateMedia(tx, job)
		return err
	})
	if err != nil {
		return receipt, err, true
	}
	receipt, err = adapter.PublishPrivateMedia(ctx, feishu.ProjectPublishRequest{
		ChatID: job.Projection.ChatID, ReplyToMessageID: job.Projection.ReplyToMessageID, UUID: uuid,
	}, media.Kind, key)
	return receipt, err, false
}

var errMediaUploadUncertain = errors.New("media upload result is unknown")

func (p *Publisher) validatePrivateMedia(tx *workstore.Tx, job *publication) (work.Content, error) {
	scope, err := tx.GetScope(job.Operation.ScopeID)
	if err != nil {
		return work.Content{}, err
	}
	if scope.Kind != work.ScopePersonal || job.ReplyInThread {
		return work.Content{}, workservice.ErrPublicationDenied
	}
	if err = workservice.ValidatePublication(tx, job.Operation, job.Projection); err != nil {
		return work.Content{}, err
	}
	if job.Projection.PrivateDisplay != nil {
		probe := *job
		if err = p.resolvePrivateDisplay(tx, job.Operation, job.Outbox, &probe, false); err != nil {
			return work.Content{}, err
		}
		if probe.TerminalReason != "" {
			return work.Content{}, fmt.Errorf("%w: %s", workservice.ErrPublicationDenied, probe.TerminalReason)
		}
	}
	return workservice.PrivateMediaContent(tx, scope.ID, *job.Projection.Media)
}

func (p *Publisher) recordMediaUpload(tx *workstore.Tx, job *publication, state work.OperationState, key string) error {
	op, err := tx.GetOperation(job.Operation.ID)
	if err != nil {
		return err
	}
	if op.Revision != job.Operation.Revision || op.State != work.OperationSending {
		return workstore.ErrConflict
	}
	step := work.OperationStep{ID: "media_upload", State: state, Receipt: key}
	index := len(op.Steps)
	for i, existing := range op.Steps {
		if existing.ID == step.ID {
			index = i
			break
		}
	}
	if index == len(op.Steps) {
		op.Steps = append(op.Steps, step)
	} else {
		op.Steps[index] = step
	}
	old := op.Revision
	op.Revision++
	op.UpdatedAt = p.now()
	if _, _, err = tx.PutOperation(op, old); err != nil {
		return err
	}
	job.Operation = op
	return nil
}
