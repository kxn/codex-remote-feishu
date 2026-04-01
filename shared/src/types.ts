/**
 * Core type definitions for the Codex Relay system.
 */

/** Session lifecycle states as tracked by the relay server. */
export type SessionState = "idle" | "executing" | "waitingApproval";

/** Classified message types flowing through the wrapper. */
export type MessageType =
  | "agentMessage"
  | "toolCall"
  | "serverRequest"
  | "turnLifecycle"
  | "threadLifecycle"
  | "unknown";

/** Envelope for WebSocket messages between wrapper and relay server. */
export interface WsMessage {
  /** Message type for routing/filtering. */
  type: string;
  /** Unique session identifier. */
  sessionId: string;
  /** Payload data. */
  payload: unknown;
}

/** Standard REST API response envelope. */
export interface ApiResponse<T = unknown> {
  /** Whether the request succeeded. */
  ok: boolean;
  /** Response data (present on success). */
  data?: T;
  /** Error message (present on failure). */
  error?: string;
}

/** Approval request identifiers follow JSON-RPC request id semantics. */
export type ApprovalRequestId = string | number;

/** Explicit approval decisions relayed back to codex. */
export type ApprovalDecision = "accept" | "decline";

/** Server-to-wrapper approval response relay payload. */
export interface ApprovalResponseRelayMessage {
  /** Wire discriminator for approval responses. */
  type: "approval-response";
  /** Original JSON-RPC request id from codex. */
  requestId: ApprovalRequestId;
  /** Approval decision to return to codex. */
  decision: ApprovalDecision;
}

/** Base shape for relay event polling responses consumed by the bot. */
export interface RelayEventBase {
  /** Monotonic event identifier for polling cursors. */
  id: number;
  /** When the relay observed the event. */
  occurredAt: string;
  /** Session identifier associated with the event. */
  sessionId: string;
  /** Human-readable session name. */
  displayName: string;
}

/** Lightweight notification that a turn completed for a session. */
export interface RelayTurnCompletedEvent extends RelayEventBase {
  type: "turn-completed";
  /** Updated turn count after completion. */
  turnCount: number;
}

/** Lightweight notification that Codex is waiting for user approval/input. */
export interface RelayInputRequiredEvent extends RelayEventBase {
  type: "input-required";
  /** Optional request identifier if the relay could parse one. */
  requestId?: ApprovalRequestId;
}

/** User-specific auto-detach event triggered by local VS Code input. */
export interface RelayAutoDetachEvent extends RelayEventBase {
  type: "auto-detach";
  /** Feishu user that was detached. */
  userId: string;
  /** Relay-provided detach reason. */
  reason: string;
}

/** User-specific session offline event triggered when a wrapper disconnects. */
export interface RelaySessionOfflineEvent extends RelayEventBase {
  type: "session-offline";
  /** Feishu user still attached when the session went offline. */
  userId: string;
  /** Grace-period deadline for reconnect, if present. */
  graceExpiresAt: string | null;
}

/** Union of relay events available through the polling API. */
export type RelayEvent =
  | RelayTurnCompletedEvent
  | RelayInputRequiredEvent
  | RelayAutoDetachEvent;

/** Polling response for the relay event stream. */
export interface RelayEventBatch {
  /** Latest event ID currently retained by the relay. */
  latestEventId: number;
  /** Events strictly newer than the requested cursor. */
  events: RelayEvent[];
}
