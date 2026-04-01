# Codex Relay — Architecture

Remote monitoring and interaction with OpenAI Codex CLI sessions via Feishu (Lark) bot.

## Component Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│  VS Code (Codex Extension)                                          │
│                                                                     │
│   Extension ←──stdio──→ Wrapper (Rust) ←──stdio──→ codex CLI       │
│                            │                                        │
└────────────────────────────┼────────────────────────────────────────┘
                             │ WebSocket (JSONL classified messages)
                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Relay Server (TypeScript / Node.js)                                │
│                                                                     │
│   Session Registry ─── Message History Buffer ─── WS Hub           │
│   (online/offline/evicted)     (per-session)      (wrapper conns)  │
│                                                                     │
│   REST API ◄──────────────────────────────────────────────────┐     │
│   (sessions, messages, commands)                              │     │
└───────────────────────────────────────────────────────────────┼─────┘
                                                                │
                                  REST + cursor event feed/API │
                                                                │
┌───────────────────────────────────────────────────────────────┼─────┐
│  Feishu Bot (TypeScript / Node.js)                            │     │
│                                                               ▼     │
│   Command Parser ─── Attach State (user→session) ─── Formatter     │
│                                                                     │
│   Feishu WSClient (@larksuiteoapi/node-sdk)                         │
│        ▲                                                            │
└────────┼────────────────────────────────────────────────────────────┘
         │ WebSocket long connection
         ▼
┌─────────────────┐
│  Feishu / Lark   │
│  (end users)     │
└─────────────────┘
```

## Components

### 1. Wrapper (Rust binary)

A stdio man-in-the-middle proxy that sits between VS Code's Codex extension and the real `codex` CLI binary.

- Spawns the real `codex` as a child process.
- Intercepts bidirectional stdio traffic (JSONL — newline-delimited JSON-RPC 2.0).
- Classifies outbound messages: `agentMessage`, `toolCall`, `approval`, `turnLifecycle`.
- Tracks `threadId` / `turnId` for session context.
- Connects to the relay server via WebSocket; sends classified messages upstream.
- Detects VS Code-side keyboard/mouse input to auto-detach remote observers.
- **Critical invariant**: all messages are forwarded transparently to VS Code — never dropped or modified. Added latency must be <5ms.

### 2. Relay Server (TypeScript / Node.js)

Central hub that bridges wrappers and the bot.

- Accepts WebSocket connections from wrappers.
- Maintains a session registry with three states:
  - `online` — wrapper connected and active.
  - `offline` — wrapper disconnected, within grace period (reconnect window).
  - `evicted` — grace period expired, session purged via LRU.
- Caches recent message history per session in a configurable ring buffer.
- Exposes a REST API consumed by the bot:
  - List sessions, get session status/history.
  - Expose a per-user event feed at `/users/:id/events`; `POST /sessions/:id/attach`
    returns a `userEventCursor` so attached forwarding can resume losslessly from a
    known point.
  - Send prompts and approval responses to a session.
- Bridges real-time messages: wrapper → server → bot (if a user is attached).
- Handles lifecycle: registration, heartbeat, disconnect, reconnect, LRU eviction.

### 3. Feishu Bot (TypeScript / Node.js)

Independent service that provides the user-facing interface via Feishu/Lark.

- Connects to Feishu via `@larksuiteoapi/node-sdk` WSClient (WebSocket long connection).
- Connects to relay server via REST API plus cursor-based `/users/:id/events`
  polling for attached-session forwarding.
- Parses user commands:
  - `/list` — show available sessions
  - `/attach <session>` — start watching a session
  - `/detach` — stop watching
  - `/stop` — interrupt the current turn
  - `/status` — session info
  - `/history` — recent message history
- Forwards free-text as prompts and approval responses to the relay server.
- Manages per-user attach state (which user → which session).
- Keeps an in-memory per-user/per-session notification route map so detached users
  only receive background notifications for sessions they previously attached to.
- Sends notifications on session state changes (online/offline/evicted).
- **In-memory only**: attachment state, notification routes, pending approvals, and
  event cursors are not persisted across bot restarts.

### 4. Shared Types (TypeScript)

Common type definitions used by the relay server and bot:

- WebSocket message envelopes and classified message types.
- REST API request/response payloads.
- Session state enums (`online`, `offline`, `evicted`).

## Data Flows

### Codex → Feishu user (monitoring)

```
codex stdout
  → Wrapper stdout parser (JSONL)
  → classify message (agentMessage | toolCall | approval | turnLifecycle)
  → forward original bytes to VS Code stdout (transparent, <5ms)
  → send classified message to Relay Server via WebSocket
  → server stores in history buffer
  → if a user is attached: bot consumes the per-user `/users/:id/events` feed from
    the attach-time `userEventCursor`
  → bot formats message → sends to Feishu user
```

### Feishu user → Codex (interaction)

```
Feishu user sends text
  → Feishu Bot receives via WSClient
  → bot parses: command or prompt?
     ├─ command (/list, /attach, etc.) → execute locally against REST API
     └─ prompt or approval → POST to Relay Server REST API
  → server routes to wrapper via WebSocket
  → wrapper writes to codex stdin (as valid JSONL)
```

### VS Code → Codex (passthrough)

```
VS Code extension stdout
  → Wrapper stdin parser (JSONL)
  → forward to codex stdin (transparent)
  → optionally send to server via WS (for context tracking)
```

## Protocol Summary

The wrapper speaks the **Codex App Server protocol**: bidirectional JSON-RPC 2.0 over stdio with JSONL framing (newline-delimited). The `"jsonrpc":"2.0"` header is omitted on the wire.

### Key methods

| Direction | Method | Purpose |
|-----------|--------|---------|
| → codex | `turn/start` | Begin a turn with user prompt |
| → codex | `turn/interrupt` | Cancel in-flight turn |
| → codex | `turn/steer` | Append input to active turn |
| ← codex | `turn/started` | Turn lifecycle: started |
| ← codex | `turn/completed` | Turn lifecycle: completed |
| ← codex | `item/started` | Item lifecycle begin (agentMessage, commandExecution, fileChange, dynamicToolCall) |
| ← codex | `item/completed` | Item lifecycle end |
| ← codex | `item/agentMessage/delta` | Streaming reasoning text |
| ← codex | `thread/started` | New thread created |
| ← codex | `serverRequest/*` | Approval requests (tool execution, file changes) |

## Key Invariants

1. **Transparent forwarding**: The wrapper never drops or modifies messages between VS Code and codex. It is a read-only tap with <5ms added latency.
2. **Server is source of truth**: All session state (online/offline/evicted) lives in the relay server. Wrappers and bot defer to it.
3. **Bot state is in-memory only**: The bot keeps ephemeral attachment mappings,
   per-session notification routes, pending approvals, and event cursors; none of
   that state survives a restart.
4. **Graceful reconnect**: When a wrapper disconnects, the session enters `offline` with a grace period. If the wrapper reconnects in time, the session resumes. Otherwise it is LRU-evicted.
5. **Single attach**: A Feishu user can be attached to at most one session at a time.
