package translator

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// Result holds the output of translating a single protocol message.
type Result struct {
	Events          []agentproto.Event
	OutboundToAgent [][]byte
	Suppress        bool
}

// Translator is the interface that agent-specific adapters implement.
// Both Codex and Claude translators satisfy this interface, allowing the
// wrapper to be agent-agnostic.
type Translator interface {
	// TranslateCommand converts a canonical agentproto command into native
	// agent protocol frames to write to the agent's stdin.
	TranslateCommand(command agentproto.Command) ([][]byte, error)

	// ObserveServer translates a line from the agent's stdout into canonical
	// events and optional follow-up frames to write back to the agent.
	ObserveServer(raw []byte) (Result, error)

	// ObserveClient translates a line from the parent's stdin into canonical events.
	ObserveClient(raw []byte) (Result, error)

	// BootstrapFrames returns synthetic frames to send to the agent on startup.
	// For Codex: the headless initialize frame.
	// For Claude: the SDK initialize control_request.
	BootstrapFrames(source string, version string) ([][]byte, error)

	// SetDebugLogger configures debug logging.
	SetDebugLogger(debugLog func(string, ...any))
}
