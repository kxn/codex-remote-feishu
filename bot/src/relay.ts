import { z } from "zod";

import type {
  MessageType,
  RelayEvent,
  RelayEventBatch,
  SessionState,
} from "shared";

const sessionStateSchema = z.enum(["idle", "executing", "waitingApproval"]);
const messageTypeSchema = z.enum([
  "agentMessage",
  "toolCall",
  "serverRequest",
  "turnLifecycle",
  "threadLifecycle",
  "unknown",
]);
const messageDirectionSchema = z.enum(["in", "out"]);

export interface RelayHistoryEntry {
  direction: "in" | "out";
  classification: MessageType;
  method?: string;
  raw: string;
  payload?: unknown;
  threadId?: string | null;
  turnId?: string | null;
  receivedAt: string;
}

export interface RelaySessionSummary {
  sessionId: string;
  displayName: string;
  state: SessionState;
  online: boolean;
  turnCount: number;
  threadId: string | null;
  turnId: string | null;
  attachedUser: string | null;
  metadata: Record<string, unknown>;
  graceExpiresAt: string | null;
}

export interface RelaySessionDetail extends RelaySessionSummary {
  historySize: number;
  lastMessage: RelayHistoryEntry | null;
  userEventCursor?: number;
}

export interface RelayUserMessageEvent {
  type: "message";
  id: number;
  occurredAt: string;
  userId: string;
  sessionId: string;
  displayName: string;
  message: RelayHistoryEntry;
}

export interface RelayUserAutoDetachEvent {
  type: "auto-detach";
  id: number;
  occurredAt: string;
  userId: string;
  sessionId: string;
  displayName: string;
  reason: string;
}

export type RelayUserEvent = RelayUserMessageEvent | RelayUserAutoDetachEvent;

export interface RelayUserEventBatch {
  latestEventId: number;
  events: RelayUserEvent[];
}

export interface RelayClientOptions {
  baseUrl: string;
  fetch?: typeof fetch;
}

export const RELAY_UNAVAILABLE_MESSAGE =
  "Relay server is unavailable, please try again later.";

export class RelayClientError extends Error {
  public readonly status: number;

  constructor(message: string, status: number, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "RelayClientError";
    this.status = status;
  }
}

export class RelayClient {
  private readonly baseUrl: string;

  private readonly fetchImplementation: typeof fetch;

  constructor(options: RelayClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/+$/, "");
    this.fetchImplementation = options.fetch ?? fetch;
  }

  async listSessions(): Promise<RelaySessionSummary[]> {
    return this.request("/sessions", {
      method: "GET",
      responseSchema: z.array(sessionSummarySchema),
    });
  }

  async getSession(sessionId: string): Promise<RelaySessionDetail> {
    return this.request(`/sessions/${encodeURIComponent(sessionId)}`, {
      method: "GET",
      responseSchema: sessionDetailSchema,
    });
  }

  async getHistory(
    sessionId: string,
    limit?: number,
  ): Promise<RelayHistoryEntry[]> {
    const search = new URLSearchParams();
    if (limit !== undefined) {
      search.set("limit", String(limit));
    }

    return this.request(
      `/sessions/${encodeURIComponent(sessionId)}/history${
        search.size > 0 ? `?${search.toString()}` : ""
      }`,
      {
        method: "GET",
        responseSchema: z.array(historyEntrySchema),
      },
    );
  }

  async listEvents(afterEventId?: number): Promise<RelayEventBatch> {
    const search = new URLSearchParams();
    if (afterEventId !== undefined) {
      search.set("after", String(afterEventId));
    }

    return this.request(`/events${search.size > 0 ? `?${search.toString()}` : ""}`, {
      method: "GET",
      responseSchema: relayEventBatchSchema,
    });
  }

  async listUserEvents(
    userId: string,
    afterEventId?: number,
  ): Promise<RelayUserEventBatch> {
    const search = new URLSearchParams();
    if (afterEventId !== undefined) {
      search.set("after", String(afterEventId));
    }

    return this.request(
      `/users/${encodeURIComponent(userId)}/events${
        search.size > 0 ? `?${search.toString()}` : ""
      }`,
      {
        method: "GET",
        responseSchema: relayUserEventBatchSchema,
      },
    );
  }

  async sendInput(
    sessionId: string,
    input:
      | { type: "prompt"; content: string }
      | { type: "approval"; requestId: string | number; approved: boolean },
  ): Promise<void> {
    await this.request(`/sessions/${encodeURIComponent(sessionId)}/input`, {
      method: "POST",
      body: input,
      responseSchema: okResponseSchema,
    });
  }

  async sendPrompt(sessionId: string, content: string): Promise<void> {
    await this.sendInput(sessionId, {
      type: "prompt",
      content,
    });
  }

  async sendApproval(
    sessionId: string,
    requestId: string | number,
    approved: boolean,
  ): Promise<void> {
    await this.sendInput(sessionId, {
      type: "approval",
      requestId,
      approved,
    });
  }

  async interrupt(sessionId: string): Promise<void> {
    await this.request(`/sessions/${encodeURIComponent(sessionId)}/interrupt`, {
      method: "POST",
      responseSchema: okResponseSchema,
    });
  }

  async attach(
    sessionId: string,
    userId: string,
  ): Promise<RelaySessionDetail> {
    return this.request(`/sessions/${encodeURIComponent(sessionId)}/attach`, {
      method: "POST",
      body: {
        userId,
      },
      responseSchema: sessionDetailSchema,
    });
  }

  async detach(sessionId: string): Promise<RelaySessionDetail> {
    return this.request(`/sessions/${encodeURIComponent(sessionId)}/detach`, {
      method: "POST",
      responseSchema: sessionDetailSchema,
    });
  }

  private async request<T>(
    path: string,
    options: {
      method: "GET" | "POST";
      body?: unknown;
      responseSchema: z.ZodType<T>;
    },
  ): Promise<T> {
    let response: Response;
    try {
      response = await this.fetchImplementation(`${this.baseUrl}${path}`, {
        method: options.method,
        headers:
          options.body === undefined
            ? undefined
            : {
                "content-type": "application/json",
              },
        body:
          options.body === undefined ? undefined : JSON.stringify(options.body),
      });
    } catch (error) {
      throw new RelayClientError(RELAY_UNAVAILABLE_MESSAGE, 503, {
        cause: error,
      });
    }

    const payload = await parseJsonResponse(response);
    if (!response.ok) {
      const errorMessage =
        relayErrorSchema.safeParse(payload).success &&
        relayErrorSchema.parse(payload).error.length > 0
          ? relayErrorSchema.parse(payload).error
          : `Relay request failed with status ${response.status}`;

      throw new RelayClientError(errorMessage, response.status);
    }

    const parsed = options.responseSchema.safeParse(payload);
    if (!parsed.success) {
      throw new Error(
        `Invalid relay response from ${path}: ${parsed.error.message}`,
      );
    }

    return parsed.data;
  }
}

const historyEntrySchema = z.object({
  direction: messageDirectionSchema,
  classification: messageTypeSchema,
  method: z.string().min(1).optional(),
  raw: z.string(),
  payload: z.unknown().optional(),
  threadId: z.string().nullable().optional(),
  turnId: z.string().nullable().optional(),
  receivedAt: z.string().datetime(),
});

const sessionSummarySchema = z.object({
  sessionId: z.string().min(1),
  displayName: z.string().min(1),
  state: sessionStateSchema,
  online: z.boolean(),
  turnCount: z.number().int().min(0),
  threadId: z.string().nullable(),
  turnId: z.string().nullable(),
  attachedUser: z.string().nullable(),
  metadata: z.record(z.unknown()),
  graceExpiresAt: z.string().datetime().nullable(),
});

const sessionDetailSchema = sessionSummarySchema.extend({
  historySize: z.number().int().min(0),
  lastMessage: historyEntrySchema.nullable(),
  userEventCursor: z.number().int().min(0).optional(),
});

const okResponseSchema = z.object({
  ok: z.literal(true),
});

const relayErrorSchema = z.object({
  error: z.string().min(1),
});

const approvalRequestIdSchema = z.union([z.string().min(1), z.number().finite()]);

const relayEventBaseSchema = z.object({
  id: z.number().int().min(0),
  occurredAt: z.string().datetime(),
  sessionId: z.string().min(1),
  displayName: z.string().min(1),
});

const relayEventSchema: z.ZodType<RelayEvent> = z.discriminatedUnion("type", [
  relayEventBaseSchema.extend({
    type: z.literal("turn-completed"),
    turnCount: z.number().int().min(0),
  }),
  relayEventBaseSchema.extend({
    type: z.literal("input-required"),
    requestId: approvalRequestIdSchema.optional(),
  }),
  relayEventBaseSchema.extend({
    type: z.literal("auto-detach"),
    userId: z.string().min(1),
    reason: z.string().min(1),
  }),
]);

const relayEventBatchSchema: z.ZodType<RelayEventBatch> = z.object({
  latestEventId: z.number().int().min(0),
  events: z.array(relayEventSchema),
});


const relayUserEventSchema: z.ZodType<RelayUserEvent> = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("message"),
    id: z.number().int().min(0),
    occurredAt: z.string().datetime(),
    userId: z.string().min(1),
    sessionId: z.string().min(1),
    displayName: z.string().min(1),
    message: historyEntrySchema,
  }),
  z.object({
    type: z.literal("auto-detach"),
    id: z.number().int().min(0),
    occurredAt: z.string().datetime(),
    userId: z.string().min(1),
    sessionId: z.string().min(1),
    displayName: z.string().min(1),
    reason: z.string().min(1),
  }),
]);

const relayUserEventBatchSchema = z.object({
  latestEventId: z.number().int().min(0),
  events: z.array(relayUserEventSchema),
});
async function parseJsonResponse(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text.length === 0) {
    return undefined;
  }

  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new Error(`Invalid JSON response from relay: ${text}`);
  }
}
