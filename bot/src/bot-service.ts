import { parseIncomingText } from "./commands.js";
import { formatFeishuMessageChunks, formatSessionTag } from "./formatter.js";
import type { RelayEvent } from "shared";
import type {
  RelayHistoryEntry,
  RelaySessionDetail,
  RelaySessionSummary,
  RelayUserEvent,
} from "./relay.js";
import { RelayClientError } from "./relay.js";

export interface IncomingTextMessage {
  userId: string;
  chatId: string;
  messageId: string;
  text: string;
}

export interface IncomingMenuAction {
  userId: string;
  eventKey: string;
}

export interface BotMessenger {
  sendText: (chatId: string, text: string) => Promise<void>;
}

export interface RelayClientLike {
  listSessions: () => Promise<RelaySessionSummary[]>;
  listEvents: (afterEventId?: number) => Promise<{
    latestEventId: number;
    events: RelayEvent[];
  }>;
  listUserEvents: (
    userId: string,
    afterEventId?: number,
  ) => Promise<{
    latestEventId: number;
    events: RelayUserEvent[];
  }>;
  getSession: (sessionId: string) => Promise<RelaySessionDetail>;
  getHistory: (
    sessionId: string,
    limit?: number,
  ) => Promise<RelayHistoryEntry[]>;
  sendPrompt: (sessionId: string, content: string) => Promise<void>;
  sendApproval: (
    sessionId: string,
    requestId: string | number,
    approved: boolean,
  ) => Promise<void>;
  interrupt: (sessionId: string) => Promise<void>;
  attach: (sessionId: string, userId: string) => Promise<RelaySessionDetail>;
  detach: (sessionId: string) => Promise<RelaySessionDetail>;
}

interface UserAttachment {
  sessionId: string;
  sessionName: string;
  chatId: string;
}

interface PendingApproval {
  sessionId: string;
  sessionName: string;
  chatId: string;
  requestId: string | number;
  signature: string;
}

interface AttachmentForwarder {
  sessionId: string;
  cursor: number | undefined;
  polling: boolean;
  inFlight: Promise<void> | null;
  timer: ReturnType<typeof setTimeout> | null;
}

interface BotServiceOptions {
  pollIntervalMs?: number;
  timerApi?: {
    clearTimeout: typeof clearTimeout;
    setTimeout: typeof setTimeout;
  };
}

export class BotService {
  private readonly attachments = new Map<string, UserAttachment>();

  private readonly notificationChats = new Map<string, Map<string, string>>();

  private readonly lastSeenChats = new Map<string, string>();

  private readonly pendingApprovals = new Map<string, PendingApproval>();

  private readonly forwarders = new Map<string, AttachmentForwarder>();

  private eventCursor: number | null = null;

  private eventCursorInitialization: Promise<void> | null = null;

  private eventPolling = false;

  private eventTimer: ReturnType<typeof setTimeout> | null = null;

  private readonly pollIntervalMs: number;

  private readonly timerApi: {
    clearTimeout: typeof clearTimeout;
    setTimeout: typeof setTimeout;
  };

  constructor(
    private readonly relayClient: RelayClientLike,
    private readonly messenger: BotMessenger,
    options: BotServiceOptions = {},
  ) {
    this.pollIntervalMs = Math.max(1, options.pollIntervalMs ?? 1_000);
    this.timerApi = options.timerApi ?? {
      clearTimeout: globalThis.clearTimeout.bind(globalThis),
      setTimeout: globalThis.setTimeout.bind(globalThis),
    };
  }

  async handleTextMessage(message: IncomingTextMessage): Promise<void> {
    this.lastSeenChats.set(message.userId, message.chatId);
    const parsed = parseIncomingText(message.text);

    try {
      await this.ensureEventCursorInitialized();
      this.ensureEventPollingStarted();

      switch (parsed.kind) {
        case "invalid":
          await this.reply(message.chatId, parsed.error);
          return;
        case "unknown-command":
          await this.reply(message.chatId, `Unknown command "/${parsed.command}".`);
          return;
        case "list":
          await this.handleList(message.chatId);
          return;
        case "attach":
          await this.handleAttach(message.userId, message.chatId, parsed.sessionQuery);
          return;
        case "detach":
          await this.handleDetach(message.userId, message.chatId);
          return;
        case "stop":
          await this.handleStop(message.userId, message.chatId);
          return;
        case "status":
          await this.handleStatus(message.userId, message.chatId);
          return;
        case "history":
          await this.handleHistory(message.userId, message.chatId, parsed.limit);
          return;
        case "prompt":
          await this.handlePrompt(message.userId, message.chatId, parsed.content);
          return;
      }
    } catch (error) {
      await this.reply(message.chatId, this.formatErrorMessage(error));
    }
  }

  async handleMenuAction(action: IncomingMenuAction): Promise<void> {
    const chatId =
      this.attachments.get(action.userId)?.chatId ??
      this.lastSeenChats.get(action.userId);
    if (!chatId) {
      return;
    }

    const normalizedAction = normalizeMenuActionKey(action.eventKey);

    try {
      await this.ensureEventCursorInitialized();
      this.ensureEventPollingStarted();

      switch (normalizedAction) {
        case "list":
          await this.handleList(chatId);
          return;
        case "stop":
          await this.handleStop(action.userId, chatId);
          return;
        case "detach":
          await this.handleDetach(action.userId, chatId);
          return;
        default:
          await this.reply(
            chatId,
            `Unknown menu action "${action.eventKey}".`,
          );
      }
    } catch (error) {
      await this.reply(chatId, this.formatErrorMessage(error));
    }
  }

  getAttachment(userId: string): UserAttachment | undefined {
    return this.attachments.get(userId);
  }

  close(): void {
    for (const userId of this.forwarders.keys()) {
      this.stopForwarding(userId);
    }

    if (this.eventTimer) {
      this.timerApi.clearTimeout(this.eventTimer);
      this.eventTimer = null;
    }
  }

  private async ensureEventCursorInitialized(): Promise<void> {
    if (this.eventCursor !== null) {
      return;
    }

    if (!this.eventCursorInitialization) {
      this.eventCursorInitialization = (async () => {
        try {
          const batch = await this.relayClient.listEvents();
          this.eventCursor = batch.latestEventId;
        } catch {
          // Event polling is best-effort; command handling should continue.
        } finally {
          this.eventCursorInitialization = null;
        }
      })();
    }

    await this.eventCursorInitialization;
  }

  private ensureEventPollingStarted(): void {
    if (!this.hasNotificationChats() || this.eventTimer || this.eventPolling) {
      return;
    }

    this.scheduleEventPolling();
  }

  private scheduleEventPolling(): void {
    if (!this.hasNotificationChats() || this.eventTimer) {
      return;
    }

    this.eventTimer = this.timerApi.setTimeout(() => {
      void this.pollEvents();
    }, this.pollIntervalMs);
  }

  private async pollEvents(): Promise<void> {
    if (this.eventPolling) {
      this.scheduleEventPolling();
      return;
    }

    this.eventPolling = true;
    this.eventTimer = null;

    try {
      await this.ensureEventCursorInitialized();
      if (this.eventCursor === null) {
        return;
      }

      const batch = await this.relayClient.listEvents(this.eventCursor);
      this.eventCursor = batch.latestEventId;

      for (const event of batch.events) {
        await this.handleRelayEvent(event);
      }
    } catch {
      // Best-effort polling; user-facing commands already surface relay errors.
    } finally {
      this.eventPolling = false;
      this.scheduleEventPolling();
    }
  }

  private async handleRelayEvent(event: RelayEvent): Promise<void> {
    if (event.type === "auto-detach") {
      const attachment = this.attachments.get(event.userId);
      if (!attachment || attachment.sessionId !== event.sessionId) {
        return;
      }

      this.stopForwarding(event.userId);
      this.attachments.delete(event.userId);
      await this.reply(
        attachment.chatId,
        "Auto-detached: local input detected.",
      );
      return;
    }

    const message =
      event.type === "turn-completed"
        ? `${formatSessionTag(event.displayName)} Turn completed.`
        : `${formatSessionTag(event.displayName)} Input required.`;

    for (const [userId, chatsBySession] of this.notificationChats) {
      const chatId = chatsBySession.get(event.sessionId);
      if (!chatId) {
        continue;
      }

      if (this.attachments.get(userId)?.sessionId === event.sessionId) {
        continue;
      }

      await this.reply(chatId, message);
    }
  }

  private async handleList(chatId: string): Promise<void> {
    const sessions = await this.relayClient.listSessions();
    if (sessions.length === 0) {
      await this.reply(chatId, "No sessions available.");
      return;
    }

    const lines = sessions.map((session) => {
      const availability = session.online ? "online" : "offline";
      return `- ${session.displayName} (${session.sessionId}) — ${session.state}, ${availability}`;
    });

    await this.reply(chatId, lines.join("\n"));
  }

  private async handleAttach(
    userId: string,
    chatId: string,
    sessionQuery: string,
  ): Promise<void> {
    const sessions = await this.relayClient.listSessions();
    const matchedSession = resolveSessionMatch(sessions, sessionQuery);
    if (matchedSession.kind === "not_found") {
      await this.reply(chatId, `Session "${sessionQuery}" not found.`);
      return;
    }

    if (matchedSession.kind === "ambiguous") {
      await this.reply(
        chatId,
        [
          `Multiple sessions match "${sessionQuery}":`,
          ...matchedSession.sessions.map(
            (session) =>
              `- ${session.displayName} (${session.sessionId}) — ${session.state}, ${
                session.online ? "online" : "offline"
              }`,
          ),
        ].join("\n"),
      );
      return;
    }

    const previousAttachment = this.attachments.get(userId);
    const attachedSession = await this.relayClient.attach(
      matchedSession.session.sessionId,
      userId,
    );

    if (
      previousAttachment &&
      previousAttachment.sessionId !== attachedSession.sessionId
    ) {
      this.stopForwarding(userId);
      try {
        await this.relayClient.detach(previousAttachment.sessionId);
      } catch {
        // Keep the new attachment even if cleaning up the previous one fails.
      }
    }

    const attachment = {
      sessionId: attachedSession.sessionId,
      sessionName: attachedSession.displayName,
      chatId,
    };
    this.attachments.set(userId, attachment);
    this.registerNotificationChat(userId, attachedSession.sessionId, chatId);
    this.ensureEventPollingStarted();

    await this.reply(
      chatId,
      await this.formatAttachmentSummary(attachedSession),
    );

    this.startForwarding(userId, attachment, attachedSession);
  }

  private async handleDetach(userId: string, chatId: string): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "You are not attached to a session.");
      return;
    }

    await this.relayClient.detach(attachment.sessionId);
    this.stopForwarding(userId);
    this.attachments.delete(userId);
    await this.reply(chatId, `Detached from ${formatSessionTag(attachment.sessionName)}.`);
  }

  private async handleStop(userId: string, chatId: string): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "Attach to a session first with /attach <session>.");
      return;
    }

    const session = await this.relayClient.getSession(attachment.sessionId);
    this.attachments.set(userId, {
      ...attachment,
      sessionName: session.displayName,
      chatId,
    });

    if (session.state === "idle") {
      await this.reply(
        chatId,
        `Session ${formatSessionTag(session.displayName)} is already idle; nothing to stop.`,
      );
      return;
    }

    await this.relayClient.interrupt(attachment.sessionId);
    await this.reply(
      chatId,
      `Sent stop request to ${formatSessionTag(session.displayName)}.`,
    );
  }

  private async handleStatus(userId: string, chatId: string): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "Attach to a session first with /attach <session>.");
      return;
    }

    const session = await this.relayClient.getSession(attachment.sessionId);
    this.attachments.set(userId, {
      ...attachment,
      sessionName: session.displayName,
      chatId,
    });

    await this.reply(
      chatId,
      await this.formatStatusSummary(session),
    );
  }

  private async handleHistory(
    userId: string,
    chatId: string,
    limit: number | undefined,
  ): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "Attach to a session first with /attach <session>.");
      return;
    }

    const history = await this.relayClient.getHistory(
      attachment.sessionId,
      limit ?? 5,
    );
    if (history.length === 0) {
      await this.reply(
        chatId,
        `No history available for ${formatSessionTag(attachment.sessionName)}.`,
      );
      return;
    }

    const content = history
      .map((entry, index) => {
        const header = `${index + 1}. ${entry.direction} ${entry.classification}${
          entry.method ? ` ${entry.method}` : ""
        }`;
        return `${header}\n${entry.raw}`;
      })
      .join("\n\n");

    await this.reply(
      chatId,
      formatFeishuMessageChunks({
        sessionName: attachment.sessionName,
        content,
        codeBlock: true,
        language: "text",
      }),
    );
  }

  private async handlePrompt(
    userId: string,
    chatId: string,
    content: string,
  ): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "Attach to a session first with /attach <session>.");
      return;
    }

    const pendingApproval = this.pendingApprovals.get(userId);
    if (pendingApproval && pendingApproval.sessionId === attachment.sessionId) {
      await this.syncForwarding(userId, attachment.sessionId);

      const currentAttachment = this.attachments.get(userId);
      if (
        !currentAttachment ||
        currentAttachment.sessionId !== attachment.sessionId
      ) {
        return;
      }

      const currentPendingApproval = this.pendingApprovals.get(userId);
      if (
        currentPendingApproval &&
        currentPendingApproval.sessionId === currentAttachment.sessionId
      ) {
        await this.handleApprovalReply(userId, currentPendingApproval, content);
        return;
      }

      await this.relayClient.sendPrompt(currentAttachment.sessionId, content);
      return;
    }

    await this.relayClient.sendPrompt(attachment.sessionId, content);
  }

  private async formatAttachmentSummary(
    session: RelaySessionDetail,
  ): Promise<string> {
    return [
      `Attached to ${formatSessionTag(session.displayName)}.`,
      `State: ${session.state}`,
      `Turns: ${session.turnCount}`,
      `Last message: ${await this.getLastAgentMessagePreview(session)}`,
    ].join("\n");
  }

  private async formatStatusSummary(session: RelaySessionDetail): Promise<string> {
    return [
      `Session: ${formatSessionTag(session.displayName)}`,
      `Session ID: ${session.sessionId}`,
      `State: ${session.state}`,
      `Online: ${session.online ? "yes" : "no"}`,
      `Turns: ${session.turnCount}`,
      `Thread: ${session.threadId ?? "none"}`,
      `Turn: ${session.turnId ?? "none"}`,
      `Last message: ${await this.getLastAgentMessagePreview(session)}`,
    ].join("\n");
  }

  private async getLastAgentMessagePreview(
    session: RelaySessionDetail,
  ): Promise<string> {
    if (session.lastMessage?.classification === "agentMessage") {
      return buildMessagePreview(session.lastMessage);
    }

    if (session.historySize <= 0) {
      return "(none yet)";
    }

    const history = await this.relayClient.getHistory(session.sessionId);
    const lastAgentMessage = [...history]
      .reverse()
      .find((entry) => entry.classification === "agentMessage");

    return lastAgentMessage ? buildMessagePreview(lastAgentMessage) : "(none yet)";
  }

  private startForwarding(
    userId: string,
    attachment: UserAttachment,
    session: RelaySessionDetail,
  ): void {
    this.stopForwarding(userId);

    const forwarder: AttachmentForwarder = {
      sessionId: attachment.sessionId,
      cursor: session.userEventCursor,
      polling: false,
      inFlight: null,
      timer: null,
    };

    this.forwarders.set(userId, forwarder);
    this.scheduleForwarding(userId, forwarder);

    if (
      session.state === "waitingApproval" &&
      session.lastMessage?.classification === "serverRequest"
    ) {
      void this.presentApprovalRequest(userId, attachment, session.lastMessage);
    }
  }

  private stopForwarding(userId: string): void {
    const forwarder = this.forwarders.get(userId);
    if (forwarder?.timer) {
      this.timerApi.clearTimeout(forwarder.timer);
    }

    this.forwarders.delete(userId);
    this.pendingApprovals.delete(userId);
  }

  private scheduleForwarding(
    userId: string,
    forwarder: AttachmentForwarder,
  ): void {
    if (this.forwarders.get(userId) !== forwarder) {
      return;
    }

    forwarder.timer = this.timerApi.setTimeout(() => {
      void this.runScheduledForwarding(userId, forwarder.sessionId);
    }, this.pollIntervalMs);
  }

  private async runScheduledForwarding(
    userId: string,
    sessionId: string,
  ): Promise<void> {
    try {
      await this.pollForwarding(userId, sessionId);
    } catch (error) {
      console.error(
        `Background forwarding poll failed for user ${userId} on session ${sessionId}.`,
        error,
      );
    }
  }

  private async syncForwarding(
    userId: string,
    sessionId: string,
  ): Promise<void> {
    const forwarder = this.forwarders.get(userId);
    if (!forwarder || forwarder.sessionId !== sessionId) {
      return;
    }

    if (forwarder.timer) {
      this.timerApi.clearTimeout(forwarder.timer);
      forwarder.timer = null;
    }

    await this.pollForwarding(userId, sessionId);
  }

  private async pollForwarding(
    userId: string,
    sessionId: string,
  ): Promise<void> {
    const forwarder = this.forwarders.get(userId);
    const attachment = this.attachments.get(userId);
    if (
      !forwarder ||
      !attachment ||
      forwarder.sessionId !== sessionId ||
      attachment.sessionId !== sessionId
    ) {
      this.stopForwarding(userId);
      return;
    }

    if (forwarder.polling) {
      await forwarder.inFlight;
      return;
    }

    forwarder.polling = true;
    forwarder.timer = null;
    forwarder.inFlight = this.consumeForwardedUserEvents(
      userId,
      sessionId,
      forwarder,
    );

    try {
      await forwarder.inFlight;
    } finally {
      if (this.forwarders.get(userId) === forwarder) {
        forwarder.polling = false;
        forwarder.inFlight = null;
        this.scheduleForwarding(userId, forwarder);
      }
    }
  }

  private async consumeForwardedUserEvents(
    userId: string,
    sessionId: string,
    forwarder: AttachmentForwarder,
  ): Promise<void> {
    try {
      const batch = await this.relayClient.listUserEvents(userId, forwarder.cursor);

      for (const event of batch.events) {
        const currentAttachment = this.attachments.get(userId);
        const currentForwarder = this.forwarders.get(userId);
        if (
          !currentAttachment ||
          !currentForwarder ||
          currentAttachment.sessionId !== sessionId ||
          currentForwarder !== forwarder
        ) {
          this.stopForwarding(userId);
          return;
        }

        await this.handleForwardedUserEvent(userId, currentAttachment, event);
      }

      forwarder.cursor = batch.latestEventId;
    } catch (error) {
      if (
        error instanceof RelayClientError &&
        (error.status === 404 || error.status === 409)
      ) {
        this.stopForwarding(userId);
        return;
      }

      throw error;
    }
  }

  private async presentApprovalRequest(
    userId: string,
    attachment: UserAttachment,
    entry: RelayHistoryEntry,
  ): Promise<void> {
    const request = extractApprovalRequest(entry);
    if (!request) {
      return;
    }

    const existing = this.pendingApprovals.get(userId);
    if (
      existing &&
      existing.sessionId === attachment.sessionId &&
      existing.signature === request.signature
    ) {
      return;
    }

    this.pendingApprovals.set(userId, {
      sessionId: attachment.sessionId,
      sessionName: attachment.sessionName,
      chatId: attachment.chatId,
      requestId: request.requestId,
      signature: request.signature,
    });

    await this.reply(
      attachment.chatId,
      formatFeishuMessageChunks({
        sessionName: attachment.sessionName,
        content: formatApprovalPrompt(request),
      }),
    );
  }

  private async handleApprovalReply(
    userId: string,
    pendingApproval: PendingApproval,
    content: string,
  ): Promise<void> {
    const normalized = content.trim().toLowerCase();
    if (normalized !== "y" && normalized !== "n") {
      await this.reply(
        pendingApproval.chatId,
        `Reply with y to approve or n to deny the pending request for ${formatSessionTag(
          pendingApproval.sessionName,
        )}.`,
      );
      return;
    }

    await this.relayClient.sendApproval(
      pendingApproval.sessionId,
      pendingApproval.requestId,
      normalized === "y",
    );
    this.pendingApprovals.delete(userId);
    await this.reply(
      pendingApproval.chatId,
      `${
        normalized === "y" ? "Approved" : "Denied"
      } request for ${formatSessionTag(pendingApproval.sessionName)}.`,
    );
  }

  private async reply(chatId: string, message: string | string[]): Promise<void> {
    const messages = Array.isArray(message) ? message : [message];
    for (const chunk of messages) {
      await this.messenger.sendText(chatId, chunk);
    }
  }

  private formatErrorMessage(error: unknown): string {
    if (error instanceof RelayClientError) {
      return error.message;
    }

    if (error instanceof Error) {
      return error.message;
    }

    return "An unexpected bot error occurred.";
  }

  private hasNotificationChats(): boolean {
    for (const chatsBySession of this.notificationChats.values()) {
      if (chatsBySession.size > 0) {
        return true;
      }
    }

    return false;
  }

  private registerNotificationChat(
    userId: string,
    sessionId: string,
    chatId: string,
  ): void {
    let chatsBySession = this.notificationChats.get(userId);
    if (!chatsBySession) {
      chatsBySession = new Map<string, string>();
      this.notificationChats.set(userId, chatsBySession);
    }

    chatsBySession.set(sessionId, chatId);
  }

  private async handleForwardedUserEvent(
    userId: string,
    attachment: UserAttachment,
    event: RelayUserEvent,
  ): Promise<void> {
    if (event.sessionId !== attachment.sessionId) {
      return;
    }

    if (event.type === "auto-detach") {
      this.stopForwarding(userId);
      this.attachments.delete(userId);
      await this.reply(
        attachment.chatId,
        "Auto-detached: local input detected.",
      );
      return;
    }

    const entry = event.message;
    if (entry.classification !== "agentMessage") {
      if (entry.classification === "serverRequest") {
        await this.presentApprovalRequest(userId, attachment, entry);
        return;
      }

      if (
        entry.method === "turn/completed" &&
        this.pendingApprovals.get(userId)?.sessionId === attachment.sessionId
      ) {
        this.pendingApprovals.delete(userId);
      }

      return;
    }

    const content = extractRelayMessageText(entry);
    if (!content) {
      return;
    }

    await this.reply(
      attachment.chatId,
      formatFeishuMessageChunks({
        sessionName: attachment.sessionName,
        content,
      }),
    );
  }
}

function resolveSessionMatch(
  sessions: RelaySessionSummary[],
  query: string,
):
  | { kind: "matched"; session: RelaySessionSummary }
  | { kind: "ambiguous"; sessions: RelaySessionSummary[] }
  | { kind: "not_found" } {
  const normalizedQuery = query.trim().toLowerCase();
  const exactMatches = sessions.filter((session) => {
    return (
      session.sessionId.toLowerCase() === normalizedQuery ||
      session.displayName.toLowerCase() === normalizedQuery
    );
  });

  if (exactMatches.length === 1) {
    return {
      kind: "matched",
      session: exactMatches[0],
    };
  }

  if (exactMatches.length > 1) {
    return {
      kind: "ambiguous",
      sessions: sortSessions(exactMatches),
    };
  }

  const partialMatches = sessions.filter((session) => {
    return (
      session.sessionId.toLowerCase().includes(normalizedQuery) ||
      session.displayName.toLowerCase().includes(normalizedQuery)
    );
  });

  if (partialMatches.length === 1) {
    return {
      kind: "matched",
      session: partialMatches[0],
    };
  }

  if (partialMatches.length > 1) {
    return {
      kind: "ambiguous",
      sessions: sortSessions(partialMatches),
    };
  }

  return { kind: "not_found" };
}

function sortSessions(
  sessions: RelaySessionSummary[],
): RelaySessionSummary[] {
  return [...sessions].sort((left, right) => {
    return (
      left.displayName.localeCompare(right.displayName) ||
      left.sessionId.localeCompare(right.sessionId)
    );
  });
}

function buildMessagePreview(entry: RelayHistoryEntry): string {
  return truncatePreview(extractRelayMessageText(entry) ?? entry.raw);
}

function truncatePreview(text: string): string {
  const normalized = text.replace(/\s+/g, " ").trim();
  if (normalized.length === 0) {
    return "(none yet)";
  }

  if (normalized.length <= 120) {
    return normalized;
  }

  return `${normalized.slice(0, 117)}...`;
}

function extractRelayMessageText(entry: RelayHistoryEntry): string | null {
  const payloadText = extractTextFromPayload(entry.payload);
  if (payloadText) {
    return payloadText;
  }

  const rawPayload = safeParseJson(entry.raw);
  const rawPayloadText = extractTextFromPayload(rawPayload);
  if (rawPayloadText) {
    return rawPayloadText;
  }

  const raw = entry.raw.trim();
  return raw.length > 0 ? raw : null;
}

function extractTextFromPayload(payload: unknown): string | null {
  const text =
    getNestedString(payload, ["params", "delta"]) ??
    getNestedString(payload, ["params", "text"]) ??
    getNestedString(payload, ["delta"]) ??
    getNestedString(payload, ["text"]);

  if (!text) {
    return null;
  }

  return text.trim().length > 0 ? text : null;
}

function getNestedString(
  value: unknown,
  path: readonly string[],
): string | undefined {
  let current: unknown = value;

  for (const segment of path) {
    if (!current || typeof current !== "object" || Array.isArray(current)) {
      return undefined;
    }

    current = (current as Record<string, unknown>)[segment];
  }

  return typeof current === "string" ? current : undefined;
}

function getEntrySignature(entry: RelayHistoryEntry): string {
  return JSON.stringify([
    entry.receivedAt,
    entry.direction,
    entry.classification,
    entry.method ?? null,
    entry.raw,
  ]);
}

interface ApprovalRequestDetails {
  requestId: string | number;
  lines: string[];
  signature: string;
}

function normalizeMenuActionKey(eventKey: string): string {
  return eventKey.trim().replace(/^\/+/, "").toLowerCase();
}

function extractApprovalRequest(
  entry: RelayHistoryEntry,
): ApprovalRequestDetails | null {
  const rawPayload = entry.payload ?? safeParseJson(entry.raw);
  const payload = asRecord(rawPayload);
  const params = getNestedRecord(payload, ["params"]);
  const requestId =
    getApprovalRequestId(params) ?? getApprovalRequestId(payload);

  if (requestId === undefined) {
    return null;
  }

  const action = getFirstStringFromObjects([params, payload], [
    ["tool"],
    ["toolName"],
    ["action"],
    ["kind"],
    ["type"],
    ["name"],
  ]);
  const command = getFirstStringFromObjects([params, payload], [
    ["command"],
    ["commandLine"],
    ["cmd"],
    ["shellCommand"],
  ]);
  const path =
    getFirstStringFromObjects([params, payload], [
      ["path"],
      ["filePath"],
      ["file"],
      ["targetPath"],
    ]) ?? getChangePathsSummary(params ?? payload);
  const description = getFirstStringFromObjects([params, payload], [
    ["message"],
    ["reason"],
    ["description"],
    ["title"],
    ["summary"],
  ]);

  const detailLines = [
    action ? `Action: ${humanizeApprovalValue(action)}` : undefined,
    command ? `Command: ${command}` : undefined,
    path ? `Path: ${path}` : undefined,
    description ? `Details: ${description}` : undefined,
    `Request ID: ${String(requestId)}`,
  ].filter((line): line is string => line !== undefined);

  if (detailLines.length === 1) {
    detailLines.unshift(`Details: ${truncatePreview(entry.raw)}`);
  }

  return {
    requestId,
    lines: detailLines,
    signature: getEntrySignature(entry),
  };
}

function formatApprovalPrompt(request: ApprovalRequestDetails): string {
  return [
    "Approval requested.",
    ...request.lines,
    "Reply with y to approve or n to deny.",
  ].join("\n");
}

function getApprovalRequestId(
  value: Record<string, unknown> | undefined,
): string | number | undefined {
  if (!value) {
    return undefined;
  }

  const candidate = value.id ?? value.requestId;
  return typeof candidate === "string" || typeof candidate === "number"
    ? candidate
    : undefined;
}

function humanizeApprovalValue(value: string): string {
  const normalized = value
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase();

  if (normalized.length === 0) {
    return value;
  }

  return normalized.charAt(0).toUpperCase() + normalized.slice(1);
}

function getFirstStringFromObjects(
  objects: Array<Record<string, unknown> | undefined>,
  paths: readonly (readonly string[])[],
): string | undefined {
  for (const object of objects) {
    for (const path of paths) {
      const value = getNestedString(object, path);
      if (value && value.trim().length > 0) {
        return value;
      }
    }
  }

  return undefined;
}

function getChangePathsSummary(
  value: Record<string, unknown> | undefined,
): string | undefined {
  const changes = value?.changes;
  if (!Array.isArray(changes)) {
    return undefined;
  }

  const paths = changes
    .map((change) => {
      const record = asRecord(change);
      return getFirstStringFromObjects([record], [
        ["path"],
        ["filePath"],
        ["file"],
      ]);
    })
    .filter((path): path is string => path !== undefined)
    .slice(0, 3);

  if (paths.length === 0) {
    return undefined;
  }

  return paths.join(", ");
}

function asRecord(
  value: unknown,
): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function getNestedRecord(
  value: Record<string, unknown> | undefined,
  path: readonly string[],
): Record<string, unknown> | undefined {
  return asRecord(getNestedValue(value, path));
}

function getNestedValue(
  value: unknown,
  path: readonly string[],
): unknown {
  let current: unknown = value;

  for (const segment of path) {
    if (!current || typeof current !== "object" || Array.isArray(current)) {
      return undefined;
    }

    current = (current as Record<string, unknown>)[segment];
  }

  return current;
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}
