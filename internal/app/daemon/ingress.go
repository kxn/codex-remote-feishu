package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

var errIngressPumpClosed = errors.New("daemon ingress pump closed")
var errIngressQueueFull = errors.New("daemon ingress queue full")

const defaultIngressMaxPerInstance = 256

type ingressWorkKind string

const (
	ingressWorkHello      ingressWorkKind = "hello"
	ingressWorkEvents     ingressWorkKind = "events"
	ingressWorkCommandAck ingressWorkKind = "command_ack"
	ingressWorkDisconnect ingressWorkKind = "disconnect"
)

type ingressWorkItem struct {
	instanceID   string
	connectionID uint64
	kind         ingressWorkKind
	hello        *agentproto.Hello
	events       []agentproto.Event
	ack          *agentproto.CommandAck
}

type ingressQueueStats struct {
	CurrentDepth   int
	PeakDepth      int
	OverloadCount  int
	StaleDropCount int
}

type ingressPump struct {
	mu             sync.Mutex
	maxPerInstance int
	queues         map[string][]ingressWorkItem
	ready          []string
	readySet       map[string]bool
	peakDepth      map[string]int
	overloadCount  map[string]int
	staleDropCount map[string]int
	notify         chan struct{}
	closed         chan struct{}
	done           chan struct{}
}

func newIngressPump() *ingressPump {
	return &ingressPump{
		maxPerInstance: defaultIngressMaxPerInstance,
		queues:         map[string][]ingressWorkItem{},
		readySet:       map[string]bool{},
		peakDepth:      map[string]int{},
		overloadCount:  map[string]int{},
		staleDropCount: map[string]int{},
		notify:         make(chan struct{}, 1),
		closed:         make(chan struct{}),
		done:           make(chan struct{}),
	}
}

func (p *ingressPump) Enqueue(item ingressWorkItem) error {
	instanceID := strings.TrimSpace(item.instanceID)
	if instanceID == "" {
		return errors.New("daemon ingress requires instance id")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case <-p.closed:
		return errIngressPumpClosed
	default:
	}

	currentDepth := len(p.queues[instanceID])
	if p.maxPerInstance > 0 && currentDepth >= p.maxPerInstance && item.kind != ingressWorkHello && item.kind != ingressWorkDisconnect {
		p.overloadCount[instanceID]++
		return errIngressQueueFull
	}
	p.queues[instanceID] = append(p.queues[instanceID], item)
	nextDepth := currentDepth + 1
	if nextDepth > p.peakDepth[instanceID] {
		p.peakDepth[instanceID] = nextDepth
	}
	if !p.readySet[instanceID] {
		p.ready = append(p.ready, instanceID)
		p.readySet[instanceID] = true
	}
	select {
	case p.notify <- struct{}{}:
	default:
	}
	return nil
}

func (p *ingressPump) Run(ctx context.Context, process func(ingressWorkItem)) error {
	defer close(p.done)
	for {
		item, ok := p.dequeue()
		if ok {
			process(item)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.closed:
			return nil
		case <-p.notify:
		}
	}
}

func (p *ingressPump) Close() {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
}

func (p *ingressPump) Wait() {
	<-p.done
}

func (p *ingressPump) Stats(instanceID string) ingressQueueStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statsLocked(strings.TrimSpace(instanceID))
}

func (p *ingressPump) MarkStaleDrop(instanceID string) ingressQueueStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	instanceID = strings.TrimSpace(instanceID)
	if instanceID != "" {
		p.staleDropCount[instanceID]++
	}
	return p.statsLocked(instanceID)
}

func (p *ingressPump) dequeue() (ingressWorkItem, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.ready) == 0 {
		return ingressWorkItem{}, false
	}

	instanceID := p.ready[0]
	p.ready = p.ready[1:]

	queue := p.queues[instanceID]
	item := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		delete(p.queues, instanceID)
		delete(p.readySet, instanceID)
	} else {
		p.queues[instanceID] = queue
		p.ready = append(p.ready, instanceID)
	}
	return item, true
}

func (p *ingressPump) statsLocked(instanceID string) ingressQueueStats {
	return ingressQueueStats{
		CurrentDepth:   len(p.queues[instanceID]),
		PeakDepth:      p.peakDepth[instanceID],
		OverloadCount:  p.overloadCount[instanceID],
		StaleDropCount: p.staleDropCount[instanceID],
	}
}
