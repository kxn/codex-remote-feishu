// Package adapterkit holds small shared building blocks for the protocol
// adapter translators (acp / claude / codex). It consolidates the debug
// logger and request-ID plumbing that used to be copy-pasted across the three
// translator packages.
package adapterkit

import "fmt"

// DebugLogger is the embedded debug sink shared by all translators. It
// consolidates the identical debugLog field / SetDebugLogger / debugf trio
// previously living in adapter/acp, adapter/claude and adapter/codex.
type DebugLogger struct {
	debugLog func(string, ...any)
}

// SetDebugLogger installs the optional debug sink.
func (d *DebugLogger) SetDebugLogger(debugLog func(string, ...any)) {
	d.debugLog = debugLog
}

// Debugf forwards to the installed sink when present.
func (d *DebugLogger) Debugf(format string, args ...any) {
	if d.debugLog != nil {
		d.debugLog(format, args...)
	}
}

// TranslatorBase embeds DebugLogger and adds the monotonically increasing
// request/turn ID counter shared by the acp and codex translators.
type TranslatorBase struct {
	DebugLogger
	nextID int
}

// InitNextID sets the starting ID for the counter; the first NextID call
// returns start. Callers with legacy non-zero starting IDs use this to keep
// wire-visible request IDs stable.
func (b *TranslatorBase) InitNextID(start int) {
	b.nextID = start
}

// NextID returns the current ID and increments it. The first call returns 0.
func (b *TranslatorBase) NextID() int {
	value := b.nextID
	b.nextID++
	return value
}

// NextRequest builds a "relay-<prefix>-<id>" request ID.
func (b *TranslatorBase) NextRequest(prefix string) string {
	return fmt.Sprintf("relay-%s-%d", prefix, b.NextID())
}
