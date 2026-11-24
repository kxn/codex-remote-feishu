package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/nativeinspection"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/pathcompare"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// privateNativeHost is explicitly assembled for the new application. It is not
// installed in the current daemon route until source migration is complete.
type privateNativeHost struct {
	teamHistory           *workstore.Store
	startHeadless         func(relayruntime.HeadlessLaunchOptions) (int, error)
	connectionEpoch       string
	verifyMigrationResume func(context.Context, privatechat.Owner, int64, string, privatechat.MigrationResumeIntent) error
	store                 *privateNativeHostStore
	registry              *executionowner.Registry
	outbound              *executionowner.Outbound
	journal               *privatechat.Journal
	processor             *privatechat.Processor
	relay                 *relayws.Server
	hostID                string
	stateDir              string
	mu                    sync.Mutex
	drainMu               sync.Mutex
	connections           map[string]uint64
	deliveryCapabilities  map[string]privateDeliveryCapability
	sealedReady           map[string]bool
}

type privateDeliveryCapability struct {
	connection uint64
	version    int
}

var errLegacyPrivateDelivery = errors.New("private wrapper does not support required delivery facts")

func (h *privateNativeHost) privateDeliverySupported(instance string, connection uint64, kind agentproto.CommandKind) bool {
	required := agentproto.PrivateDeliveryRequirement(kind)
	if required == 0 {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	capability := h.deliveryCapabilities[instance]
	return connection != 0 && capability.connection == connection && capability.version >= required
}

func privateRegistration(o agentproto.NativeOwner) executionowner.Registration {
	return executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: o.InstanceID, Incarnation: o.CarrierIncarnation, Backend: string(o.Backend), Namespace: o.HistoryNamespace, Generation: o.InstanceGeneration}, Owner: executionowner.Owner{Application: executionowner.Private, ID: o.OwnerID, Epoch: o.Epoch}}
}

type privateNativeAuthority struct {
	host   *privateNativeHost
	scoped *executionowner.Lease
}

func (a privateNativeAuthority) WithNativeLease(ctx context.Context, n agentproto.NativeOwner, s privatechat.Owner, g int64, fn func() error) error {
	if a.host == nil || n.HostID != a.host.hostID || n.Validate() != nil {
		return fmt.Errorf("private native host mismatch")
	}
	rec, err := a.host.store.lookup(ctx, n.InstanceID)
	if err != nil {
		return err
	}
	if rec.Snapshot.Owner != n || rec.Surface != s || rec.SurfaceGeneration != g || rec.State == "retired" {
		return executionowner.ErrConflict
	}
	reg := privateRegistration(n)
	if a.scoped != nil {
		return a.scoped.Run(reg, fn)
	}
	return a.host.registry.WithLease(ctx, reg, fn)
}
func (h *privateNativeHost) lease(ctx context.Context, rec privateCarrierRecord, scoped *executionowner.Lease) (*privatechat.NativeLease, error) {
	return h.journal.OpenNativeLease(ctx, privateNativeAuthority{host: h, scoped: scoped}, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration)
}

// acceptHello requires relayws to have authenticated the exact connection with
// store.authorize before invoking this method; hello alone is not authority.
func (h *privateNativeHost) acceptHello(ctx context.Context, meta relayws.ConnectionMeta, hello agentproto.Hello) error {
	if meta.ConnectionID == 0 || !meta.PrivateAuthenticated {
		return fmt.Errorf("connection identity required")
	}
	rec, err := h.store.lookup(ctx, hello.Instance.InstanceID)
	if err != nil {
		return err
	}
	rec, err = h.recordAuthenticatedProcess(ctx, rec, hello)
	if err != nil {
		return err
	}
	lease, err := h.lease(ctx, rec, nil)
	if err != nil {
		return err
	}
	return h.relay.WithCurrentConnection(hello.Instance.InstanceID, meta.ConnectionID, func() error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.connectionEpoch == "" {
			var nonce [16]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				return err
			}
			h.connectionEpoch = hex.EncodeToString(nonce[:])
		}
		if _, err := h.processor.ApplyAuthenticatedNativeHello(ctx, lease, hello, fmt.Sprintf("%s:%d", h.connectionEpoch, meta.ConnectionID), orchestrator.PrivateNativeProfiles{Codex: rec.CodexProfileID, Claude: rec.ClaudeProfileID, OpenCode: rec.OpenCodeProfileID}); err != nil {
			return err
		}
		if h.connections == nil {
			h.connections = make(map[string]uint64)
		}
		h.connections[hello.Instance.InstanceID] = meta.ConnectionID
		if h.deliveryCapabilities == nil {
			h.deliveryCapabilities = make(map[string]privateDeliveryCapability)
		}
		h.deliveryCapabilities[hello.Instance.InstanceID] = privateDeliveryCapability{meta.ConnectionID, hello.PrivateDeliveryVersion}
		return nil
	})
}

// disconnect serializes against authenticated hello so an old socket cannot
// mark its replacement offline. Connectivity loss is not a stop proof.
func (h *privateNativeHost) disconnect(ctx context.Context, meta relayws.ConnectionMeta, instance string) error {
	if !meta.PrivateAuthenticated || meta.ConnectionID == 0 {
		return executionowner.ErrConflict
	}
	ctx = context.WithoutCancel(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.connections[instance] != meta.ConnectionID {
		return nil
	}
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	lease, err := h.lease(ctx, rec, nil)
	if err != nil {
		return err
	}
	_, err = h.processor.ApplyNativeDisconnect(ctx, lease, fmt.Sprintf("%s:%d", h.connectionEpoch, meta.ConnectionID))
	if err != nil {
		return err
	}
	delete(h.connections, instance)
	delete(h.deliveryCapabilities, instance)
	return nil
}

// recordAuthenticatedProcess accepts PID recovery only after relay credential
// authentication; the OS start identity prevents PID reuse and false adoption.
func (h *privateNativeHost) recordAuthenticatedProcess(ctx context.Context, rec privateCarrierRecord, hello agentproto.Hello) (privateCarrierRecord, error) {
	if !hello.Instance.Managed || hello.Instance.Source != "headless" || hello.Instance.InstanceID != rec.Snapshot.Owner.InstanceID || hello.CarrierIncarnation != rec.Snapshot.Owner.CarrierIncarnation || agentproto.EffectiveHelloBackend(hello) != rec.Snapshot.Owner.Backend || !pathcompare.SameCleanPlatformPath(hello.Instance.WorkspaceRoot, rec.Snapshot.CWD) {
		return rec, executionowner.ErrConflict
	}
	process, err := hostinventory.CaptureProcess(hello.Instance.PID)
	if err != nil {
		return rec, err
	}
	if rec.State == "sending" {
		if err = h.store.transition(ctx, hello.Instance.InstanceID, "sending", "running", process); err != nil {
			return rec, err
		}
		rec, err = h.store.lookup(ctx, hello.Instance.InstanceID)
		if err != nil {
			return rec, err
		}
	}
	if rec.State != "running" && rec.State != "draining" && rec.State != "sealed" || rec.Process != process {
		return rec, executionowner.ErrConflict
	}
	return rec, nil
}

func (h *privateNativeHost) validConnection(instance string, meta relayws.ConnectionMeta) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return meta.PrivateAuthenticated && meta.ConnectionID != 0 && h.connections[instance] == meta.ConnectionID
}
func (h *privateNativeHost) receive(ctx context.Context, meta relayws.ConnectionMeta, instance string, event agentproto.Event) error {
	if !h.validConnection(instance, meta) {
		return fmt.Errorf("authenticated connection required")
	}
	if event.Live != nil {
		return h.receiveLive(ctx, meta, instance, event)
	}
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	var receipt *agentproto.EventReceipt
	err = h.relay.WithCurrentConnection(instance, meta.ConnectionID, func() error {
		if !h.validConnection(instance, meta) {
			return fmt.Errorf("authenticated connection replaced")
		}
		return h.registry.WithScopedLease(ctx, privateRegistration(rec.Snapshot.Owner), func(token *executionowner.Lease) error {
			lease, e := h.lease(ctx, rec, token)
			if e != nil {
				return e
			}
			if event.CommandID != "" {
				reader := privateHostJournalReader{snapshot: rec.Snapshot, stateDir: h.stateDir}
				if e = lease.RecoverNativeCommandReceipt(ctx, reader, event.CommandID); e != nil {
					return e
				}
				if event.Kind == agentproto.EventNativeQueryCompleted && event.Metadata["sourceCommandId"] != "" {
					if e = lease.RecoverNativeQueryProof(ctx, reader, event.CommandID); e != nil {
						return e
					}
				}
			}
			receipt, e = lease.ReceiveNativeEvent(ctx, event)
			if e != nil {
				return e
			}
			proven := event.Kind == agentproto.EventNativeTurnBindingObserved && event.Status == "accepted" || event.Kind == agentproto.EventTurnStarted && event.Metadata["turnIdSource"] == "synthetic_from_observed_user_uuid"
			if proven {
				ref := privatechat.NativeEventRef{Incarnation: event.Journal.CarrierIncarnation, Sequence: event.Seq, PayloadHash: event.Journal.PayloadHash}
				session, e := lease.CommittedNativeSession(ctx, ref)
				if e != nil {
					return e
				}
				return token.BindSession(privateRegistration(rec.Snapshot.Owner), session)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	return h.relay.SendEventReceiptToConnection(instance, meta.ConnectionID, *receipt)
}
func (h *privateNativeHost) sendEffect(ctx context.Context, meta relayws.ConnectionMeta, instance, effect string, binding agentproto.NativeTurnBinding, targetSession string, expectedKind agentproto.CommandKind) error {
	if !h.validConnection(instance, meta) {
		return fmt.Errorf("authenticated connection required")
	}
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	if binding.Owner != rec.Snapshot.Owner {
		return executionowner.ErrConflict
	}
	missingCapability := !h.privateDeliverySupported(instance, meta.ConnectionID, expectedKind)
	var command agentproto.Command
	var dispatch privatechat.Dispatch
	err = h.outbound.SendLeased(ctx, privateRegistration(binding.Owner), func(token *executionowner.Lease) error {
		if expectedKind != agentproto.CommandTurnInterrupt && expectedKind != agentproto.CommandRuntimeInspect && expectedKind != agentproto.CommandProcessExit {
			if e := token.CheckBusiness(privateRegistration(binding.Owner)); e != nil {
				return e
			}
		}
		if targetSession != "" {
			if e := token.ReserveSession(privateRegistration(binding.Owner), targetSession); e != nil {
				return e
			}
		}
		lease, e := h.lease(ctx, rec, token)
		if e != nil {
			return e
		}
		dispatch, command, e = lease.PrepareEffectDispatch(ctx, effect, binding)
		if e == nil && command.Kind != expectedKind {
			return executionowner.ErrConflict
		}
		if e != nil {
			return e
		}
		if rec.State == "draining" && command.Kind != agentproto.CommandTurnInterrupt && command.Kind != agentproto.CommandRuntimeInspect {
			return fmt.Errorf("private carrier draining")
		}
		if rec.State != "running" && rec.State != "draining" {
			return fmt.Errorf("private carrier not running")
		}
		if privateEffectTargetsSession(command.Kind) && command.Target.ThreadID != targetSession || !privateEffectTargetsSession(command.Kind) && targetSession != "" {
			return executionowner.ErrConflict
		}
		if e = lease.MarkSending(ctx, dispatch.ID); e != nil {
			return e
		}
		if missingCapability {
			return errLegacyPrivateDelivery
		}
		return nil
	}, func() error { return h.relay.SendCommandToConnection(instance, meta.ConnectionID, command) })
	if err != nil && dispatch.ID != "" {
		var before *executionowner.BeforeSendError
		if errors.As(err, &before) {
			saveCtx := context.WithoutCancel(ctx)
			if e := h.registry.WithScopedLease(saveCtx, privateRegistration(binding.Owner), func(token *executionowner.Lease) error {
				lease, e := h.lease(saveCtx, rec, token)
				if e != nil {
					return e
				}
				reason := "transport_not_invoked"
				if errors.Is(err, errLegacyPrivateDelivery) {
					reason = "legacy_private_delivery"
				}
				return lease.RecordKnownUnsent(saveCtx, effect, dispatch.ID, reason)
			}); e == nil {
				return err
			}
		}
		if lease, e := h.lease(ctx, rec, nil); e == nil {
			_ = lease.MarkDispatchUnknown(ctx, dispatch.ID, err.Error())
		}
	}
	return err
}

// beginDrain commits the admission fence before any further business command.
// Interrupt and owner inspection may still be sent through the normal gate.
func (h *privateNativeHost) beginDrain(ctx context.Context, instance string) error {
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	if rec.State == "draining" {
		return nil
	}
	return h.store.transition(ctx, instance, "running", "draining", hostinventory.ProcessIdentity{})
}

// sealDrain follows the committed owner-wide inspection; afterward no socket
// command is allowed and final native/OS fences must still be verified.
func (h *privateNativeHost) sealDrain(ctx context.Context, instance string, ref privatechat.NativeEventRef) error {
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	if rec.State != "draining" {
		return fmt.Errorf("durable draining required")
	}
	lease, err := h.lease(ctx, rec, nil)
	if err != nil {
		return err
	}
	inspection, err := lease.CommittedNativeInspection(ctx, ref)
	if err != nil {
		return err
	}
	if err = nativeinspection.VerifyPrivateOwnerStoppedWithTerminalProof(rec.Snapshot.Owner, inspection, func(event agentproto.Event) bool {
		return h.journal.ProveCommittedNativeTerminal(ctx, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration, event) == nil
	}); err != nil {
		return err
	}
	if err = h.store.seal(ctx, instance, ref); err != nil {
		return err
	}
	if err = h.outbound.Drain(ctx, privateRegistration(rec.Snapshot.Owner)); err != nil {
		return err
	}
	h.markSealed(instance)
	return nil
}

func (h *privateNativeHost) markSealed(instance string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sealedReady == nil {
		h.sealedReady = make(map[string]bool)
	}
	h.sealedReady[instance] = true
}

// restoreSealed must finish before exposing senders after daemon restart.
func (h *privateNativeHost) restoreSealed(ctx context.Context) error {
	records, err := h.store.byState(ctx, "sealed")
	if err != nil {
		return err
	}
	for _, rec := range records {
		reg := privateRegistration(rec.Snapshot.Owner)
		if err = h.outbound.Drain(ctx, reg); err != nil {
			// If retirement committed before the host state update, this exact replay
			// succeeds without invoking VerifyStop. Any other error stays fail closed.
			if e := h.registry.Retire(ctx, reg, privateInspectionEvidence(rec.Inspection)); e != nil {
				return err
			}
			if e := h.store.transition(ctx, reg.Carrier.InstanceID, "sealed", "retired", hostinventory.ProcessIdentity{}); e != nil {
				return e
			}
			continue
		}
		h.markSealed(rec.Snapshot.Owner.InstanceID)
	}
	return nil
}

// sendInspection creates a host-only owner-wide query. It consumes no user
// input and can never stand in for an interrupt or prove process exit.
func (h *privateNativeHost) sendInspection(ctx context.Context, meta relayws.ConnectionMeta, instance, id string) (string, error) {
	if !h.validConnection(instance, meta) {
		return "", fmt.Errorf("authenticated connection required")
	}
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return "", err
	}
	if rec.State != "draining" {
		return "", fmt.Errorf("durable draining required")
	}
	if id == "" || rec.StopInspectionID != id || rec.StopEffectID == "" {
		return "", fmt.Errorf("persisted inspection intent required")
	}
	owner := rec.Snapshot.Owner
	binding := agentproto.NativeTurnBinding{Version: agentproto.PrivateNativeProtocolVersion, Owner: owner}
	command := agentproto.Command{CommandID: id, Kind: agentproto.CommandRuntimeInspect, NativeBinding: &binding, RuntimeInspection: &agentproto.RuntimeInspectionRequest{Version: agentproto.RuntimeInspectionProtocolVersion, ExpectedInstanceGeneration: owner.InstanceGeneration, ExpectedProcessGeneration: owner.ProcessGeneration, StopBackground: true}}
	dispatch := privatechat.Dispatch{ID: id, Owner: rec.Surface, Generation: rec.SurfaceGeneration, CommandID: id, Lane: "inspect", Mode: "start", Session: privatechat.NativeSession{Backend: string(owner.Backend), Namespace: owner.HistoryNamespace, CarrierID: owner.CarrierIncarnation, CarrierGeneration: owner.InstanceGeneration}}
	err = h.outbound.SendLeased(ctx, privateRegistration(owner), func(token *executionowner.Lease) error {
		lease, e := h.lease(ctx, rec, token)
		if e != nil {
			return e
		}
		if e = lease.PrepareHostInspection(ctx, dispatch, command); e != nil {
			return e
		}
		return lease.MarkSending(ctx, id)
	}, func() error { return h.relay.SendCommandToConnection(instance, meta.ConnectionID, command) })
	if err != nil {
		if lease, e := h.lease(ctx, rec, nil); e == nil {
			_ = lease.MarkDispatchUnknown(ctx, id, err.Error())
		}
	}
	return id, err
}

type privateHostJournalReader struct {
	snapshot agentproto.PrivateNativeLaunchSnapshot
	stateDir string
}

func (r privateHostJournalReader) ReadNativeCommandReceipt(ctx context.Context, owner agentproto.NativeOwner, id string) (*agentproto.RuntimeCommandReceipt, error) {
	if owner != r.snapshot.Owner {
		return nil, executionowner.ErrConflict
	}
	return runtimejournal.ReadPrivateCommandReceipt(ctx, r.snapshot, r.stateDir, id)
}
func (r privateHostJournalReader) ReadNativeQueryProof(ctx context.Context, owner agentproto.NativeOwner, id string) (privatechat.NativeQueryProof, error) {
	if owner != r.snapshot.Owner {
		return privatechat.NativeQueryProof{}, executionowner.ErrConflict
	}
	link, err := runtimejournal.ReadPrivateQueryLink(ctx, r.snapshot, r.stateDir, id)
	if err != nil {
		return privatechat.NativeQueryProof{}, err
	}
	return privatechat.NativeQueryProof{WaiterID: link.WaiterID, Incarnation: link.Incarnation, SourceCommandID: link.SourceCommandID, SourceCommandHash: link.SourceCommandHash, RequestID: link.RequestID, SourceReceipt: link.SourceReceipt}, nil
}

// VerifyStop is the registry's trusted verifier; it re-reads all evidence while
// the registry holds the ownership transaction. Socket drain and each domain
// are independent requirements. It never treats interrupt acceptance as stop.
func (h *privateNativeHost) VerifyStop(ctx context.Context, reg executionowner.Registration, evidence string) error {
	rec, err := h.store.lookup(ctx, reg.Carrier.InstanceID)
	if err != nil {
		return err
	}
	if rec.State == "aborted" && evidence == "unspawned:"+rec.Snapshot.Owner.CarrierIncarnation && rec.Process.PID == 0 && privateRegistration(rec.Snapshot.Owner) == reg && rec.Snapshot.Owner.HostID == h.hostID {
		return nil
	}
	if rec.State != "sealed" || privateRegistration(rec.Snapshot.Owner) != reg || rec.Snapshot.Owner.HostID != h.hostID {
		return executionowner.ErrConflict
	}
	h.mu.Lock()
	ready := h.sealedReady[reg.Carrier.InstanceID]
	h.mu.Unlock()
	if !ready {
		return fmt.Errorf("outbound drain not restored")
	}
	ref := rec.Inspection
	if evidence != privateInspectionEvidence(ref) {
		return executionowner.ErrConflict
	}
	result, err := h.journal.ReadCommittedNativeInspection(ctx, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration, ref)
	if err != nil {
		return err
	}
	if err = nativeinspection.VerifyPrivateOwnerStoppedWithTerminalProof(rec.Snapshot.Owner, result, func(event agentproto.Event) bool {
		return h.journal.ProveCommittedNativeTerminal(ctx, rec.Snapshot.Owner, rec.Surface, rec.SurfaceGeneration, event) == nil
	}); err != nil {
		return err
	}
	if err = hostinventory.WaitProcessExit(ctx, rec.Process); err != nil {
		return err
	}
	return runtimejournal.VerifyRetiredPrivateWriteFence(ctx, rec.Snapshot, h.stateDir, result.NativeWriteWatermark)
}

// receiveAck uses the acknowledgement only as a prompt to read the wrapper's
// durable command/frame journal. Transport acceptance itself is not evidence of
// native acceptance or rejection.
func (h *privateNativeHost) receiveAck(ctx context.Context, meta relayws.ConnectionMeta, instance string, ack agentproto.CommandAck) error {
	if !h.validConnection(instance, meta) || ack.CommandID == "" || (ack.InstanceID != "" && ack.InstanceID != instance) {
		return executionowner.ErrConflict
	}
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	return h.relay.WithCurrentConnection(instance, meta.ConnectionID, func() error {
		if !h.validConnection(instance, meta) {
			return executionowner.ErrConflict
		}
		lease, e := h.lease(ctx, rec, nil)
		if e != nil {
			return e
		}
		return lease.RecoverNativeCommandReceipt(ctx, privateHostJournalReader{snapshot: rec.Snapshot, stateDir: h.stateDir}, ack.CommandID)
	})
}

func privateInspectionEvidence(ref privatechat.NativeEventRef) string {
	return fmt.Sprintf("%s:%d:%s", ref.Incarnation, ref.Sequence, ref.PayloadHash)
}
func (h *privateNativeHost) retire(ctx context.Context, instance string) error {
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	if rec.State == "retired" {
		return nil
	}
	if rec.State != "sealed" {
		return fmt.Errorf("carrier not sealed")
	}
	if err = h.outbound.Retire(ctx, privateRegistration(rec.Snapshot.Owner), privateInspectionEvidence(rec.Inspection)); err != nil {
		return err
	}
	return h.store.transition(ctx, instance, "sealed", "retired", hostinventory.ProcessIdentity{})
}

func (h *privateNativeHost) receiveLive(ctx context.Context, meta relayws.ConnectionMeta, instance string, event agentproto.Event) error {
	if !h.validConnection(instance, meta) || agentproto.PrivateDeliveryForEvent(event.Kind) != agentproto.PrivateEventLive || event.Journal != nil || event.Live == nil {
		return executionowner.ErrConflict
	}
	rec, err := h.store.lookup(ctx, instance)
	if err != nil {
		return err
	}
	return h.relay.WithCurrentConnection(instance, meta.ConnectionID, func() error {
		if !h.validConnection(instance, meta) {
			return executionowner.ErrConflict
		}
		lease, e := h.lease(ctx, rec, nil)
		if e != nil {
			return e
		}
		_, e = h.processor.ApplyLiveNative(ctx, lease, event, privatechat.CoreNativeReducer{})
		return e
	})
}
