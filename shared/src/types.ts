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
