// Package workingress durably accepts verified platform envelopes before any
// message expansion or business dispatch. It never starts a native worker.
package workingress

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

type Directory interface {
	ResolveGateway(context.Context, string, string) (work.IngressGateway, error)
	ResolveLeader(context.Context, string, string, string) (string, error)
	ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error)
}
type ExpandedAttachment struct {
	Kind, MediaType, FileName, SourceMessageID string
	Data                                       []byte
}
type ExpandedMessage struct {
	Attachments []ExpandedAttachment
	Text        string
	Complete    bool
	Issues      []string
}
type Expander interface {
	Expand(context.Context, work.RawIngressEnvelope) (ExpandedMessage, error)
}

// AcceptedExpander preserves the identity generation frozen at SDK acceptance.
type AcceptedExpander interface {
	ExpandAccepted(context.Context, work.IngressRecord) (ExpandedMessage, error)
}
type PrivateCommandReceipt struct {
	Rejected    bool
	ErrorDetail string
	Receipt     string
	Unknown     bool
}
type PrivateCommandHandler interface {
	Handle(context.Context, work.TypedCommandEnvelope) (PrivateCommandReceipt, error)
}
type Config struct {
	Directory        Directory
	Expander         Expander
	PrivateCommands  PrivateCommandHandler
	Now              func() time.Time
	ExpansionTimeout time.Duration
}
type Ingress struct {
	store  *workstore.Store
	config Config
}

func New(store *workstore.Store, config Config) (*Ingress, error) {
	if store == nil || config.Directory == nil {
		return nil, fmt.Errorf("ingress requires durable store and trusted gateway directory")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ExpansionTimeout <= 0 {
		config.ExpansionTimeout = 15 * time.Second
	}
	if config.Expander == nil {
		config.Expander = DefaultExpander{}
	}
	return &Ingress{store: store, config: config}, nil
}

type ReceiveReceipt struct {
	IngressID string
	Replay    bool
}

// Receive must complete before the SDK callback returns success. All directory
// methods are trusted local configuration reads, never network discovery.
func (i *Ingress) Receive(ctx context.Context, envelope work.RawIngressEnvelope) (ReceiveReceipt, error) {
	var receipt ReceiveReceipt
	gateway, err := i.config.Directory.ResolveGateway(ctx, envelope.GatewayRef, envelope.AppID)
	if err != nil {
		return receipt, err
	}
	if envelope.ReceivedAt.IsZero() {
		envelope.ReceivedAt = i.config.Now().UTC()
	}
	err = i.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding(gateway.RoleID)
		if err != nil {
			return err
		}
		if binding.GatewayRef != gateway.GatewayRef || binding.AppID != gateway.AppID || binding.IdentityGeneration != gateway.BotGeneration {
			return fmt.Errorf("receiving gateway identity is no longer current")
		}
		record, created, err := tx.AcceptRawIngress(envelope, gateway)
		receipt = ReceiveReceipt{IngressID: record.ID, Replay: !created}
		if err != nil {
			return err
		}
		if created && envelope.ChatType == "p2p" && envelope.SenderType == "user" {
			principal, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: envelope.Tenant, AppID: envelope.AppID, Generation: gateway.BotGeneration, UnionDomain: gateway.UnionDomain, IDs: envelope.SenderIDs, ObservationRef: record.ID})
			if err != nil {
				return err
			}
			scope, err := i.resolveScope(tx, record, principal, "")
			if err != nil {
				return err
			}
			navigation, err := tx.EnsurePrivateNavigation(scope.ID, record.ID, i.config.Now().UTC())
			if err != nil {
				return err
			}
			return tx.FreezePrivateIngressNavigation(record.ID, navigation)
		}
		return nil
	})
	return receipt, err
}
func (i *Ingress) Handle(ctx context.Context, envelope work.RawIngressEnvelope) error {
	_, err := i.Receive(ctx, envelope)
	return err
}

type ProcessFailure struct {
	IngressID string
	Error     string
}
type SelectionRequest struct {
	IngressID        string
	Envelope         work.RawIngressEnvelope
	CandidateRoleIDs []string
	Reason           string
}
type ProcessResult struct {
	BlockedReplies []string
	Selections     []SelectionRequest
	Processed      int
	Partial        int
	Commands       int
	Ignored        int
	Failures       []ProcessFailure
}

func (i *Ingress) ProcessPending(ctx context.Context, limit int) (ProcessResult, error) {
	result := ProcessResult{}
	if limit < 1 || limit > 1000 {
		return result, fmt.Errorf("ingress limit must be 1..1000")
	}
	for attempt := 0; attempt < limit; attempt++ {
		var records []work.IngressRecord
		err := i.store.Within(ctx, func(tx *workstore.Tx) error {
			var err error
			records, err = tx.ListPendingIngressAt(i.config.Now(), 1)
			return err
		})
		if err != nil {
			return result, err
		}
		if len(records) == 0 {
			break
		}
		record := records[0]
		if err := ctx.Err(); err != nil {
			return result, err
		}
		expanded := ExpandedMessage{Complete: true}
		var expandErr error
		recalled := false
		if err = i.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			recalled, e = tx.IsPlatformMessageRecalled(record.Envelope.Tenant, record.Envelope.ChatID, record.Envelope.MessageID)
			return e
		}); err != nil {
			return result, err
		}
		if record.Envelope.SenderType == "user" && !recalled {
			expandCtx, cancel := context.WithTimeout(ctx, i.config.ExpansionTimeout)
			if bound, ok := i.config.Expander.(AcceptedExpander); ok {
				expanded, expandErr = bound.ExpandAccepted(expandCtx, record)
			} else {
				expanded, expandErr = i.config.Expander.Expand(expandCtx, record.Envelope)
			}
			cancel()
			if expandErr != nil {
				expanded = fallbackExpansion(record.Envelope, "引用或附件展开失败；原始消息仍已保存。")
			}
		}
		state, partial, err := i.process(ctx, record, expanded)
		if errors.Is(err, workstore.ErrConflict) {
			continue
		}
		if err != nil {
			result.Failures = append(result.Failures, ProcessFailure{IngressID: record.ID, Error: err.Error()})
			var blocked *PrivateReplyRoutingError
			if errors.As(err, &blocked) {
				result.BlockedReplies = append(result.BlockedReplies, record.ID)
			}
			var selection *LeaderSelectionError
			if errors.As(err, &selection) {
				result.Selections = append(result.Selections, SelectionRequest{IngressID: record.ID, Envelope: record.Envelope, CandidateRoleIDs: selection.Candidates, Reason: err.Error()})
			}
			_ = i.deferFailure(ctx, record.ID, err)
			continue
		}
		if state == "" {
			continue
		}
		switch state {
		case work.IngressIgnored:
			result.Ignored++
		case work.IngressCommand:
			result.Commands++
		default:
			result.Processed++
		}
		if partial {
			result.Partial++
		}
	}
	if len(result.Failures) > 0 {
		err := i.store.Within(ctx, func(tx *workstore.Tx) error {
			pending := map[string]bool{}
			for _, failure := range result.Failures {
				record, err := tx.GetIngress(failure.IngressID)
				if err != nil {
					return err
				}
				pending[failure.IngressID] = record.State == work.IngressPending
			}
			failures := result.Failures[:0]
			for _, failure := range result.Failures {
				if pending[failure.IngressID] {
					failures = append(failures, failure)
				}
			}
			result.Failures = failures
			selections := result.Selections[:0]
			for _, selection := range result.Selections {
				if pending[selection.IngressID] {
					selections = append(selections, selection)
				}
			}
			result.Selections = selections
			return nil
		})
		if err != nil {
			return result, err
		}
	}
	return result, nil
}
