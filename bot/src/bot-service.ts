import { parseIncomingText } from "./commands.js";
import { formatFeishuMessageChunks, formatSessionTag } from "./formatter.js";
import type {
  RelayHistoryEntry,
  RelaySessionDetail,
  RelaySessionSummary,
} from "./relay.js";
import { RelayClientError } from "./relay.js";

export interface IncomingTextMessage {
  userId: string;
  chatId: string;
  messageId: string;
  text: string;
}

export interface BotMessenger {
  sendText: (chatId: string, text: string) => Promise<void>;
}

export interface RelayClientLike {
  listSessions: () => Promise<RelaySessionSummary[]>;
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

interface HistoryCursor {
  historySize: number;
  receivedAt: string | null;
  signature: string | null;
}

interface AttachmentForwarder {
  sessionId: string;
  cursor: HistoryCursor;
  polling: boolean;
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

  private readonly forwarders = new Map<string, AttachmentForwarder>();

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
    const parsed = parseIncomingText(message.text);

    try {
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

  getAttachment(userId: string): UserAttachment | undefined {
    return this.attachments.get(userId);
  }

  close(): void {
    for (const userId of this.forwarders.keys()) {
      this.stopForwarding(userId);
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

    await this.relayClient.interrupt(attachment.sessionId);
    await this.reply(
      chatId,
      `Sent stop request to ${formatSessionTag(attachment.sessionName)}.`,
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
      cursor: createHistoryCursor(session),
      polling: false,
      timer: null,
    };

    this.forwarders.set(userId, forwarder);
    this.scheduleForwarding(userId, forwarder);
  }

  private stopForwarding(userId: string): void {
    const forwarder = this.forwarders.get(userId);
    if (forwarder?.timer) {
      this.timerApi.clearTimeout(forwarder.timer);
    }

    this.forwarders.delete(userId);
  }

  private scheduleForwarding(
    userId: string,
    forwarder: AttachmentForwarder,
  ): void {
    if (this.forwarders.get(userId) !== forwarder) {
      return;
    }

    forwarder.timer = this.timerApi.setTimeout(() => {
      void this.pollForwarding(userId, forwarder.sessionId);
    }, this.pollIntervalMs);
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
      this.scheduleForwarding(userId, forwarder);
      return;
    }

    forwarder.polling = true;
    forwarder.timer = null;

    try {
      const history = await this.relayClient.getHistory(sessionId);
      const newEntries = getEntriesAfterCursor(history, forwarder.cursor);

      for (const entry of newEntries) {
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

        if (entry.classification !== "agentMessage") {
          continue;
        }

        const content = extractRelayMessageText(entry);
        if (!content) {
          continue;
        }

        await this.reply(
          currentAttachment.chatId,
          formatFeishuMessageChunks({
            sessionName: currentAttachment.sessionName,
            content,
          }),
        );
      }

      forwarder.cursor = updateHistoryCursor(history, forwarder.cursor);
    } catch (error) {
      if (error instanceof RelayClientError && error.status === 404) {
        this.stopForwarding(userId);
        return;
      }
    } finally {
      if (this.forwarders.get(userId) === forwarder) {
        forwarder.polling = false;
        this.scheduleForwarding(userId, forwarder);
      }
    }
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

function createHistoryCursor(session: RelaySessionDetail): HistoryCursor {
  return {
    historySize: session.historySize,
    receivedAt: session.lastMessage?.receivedAt ?? null,
    signature: session.lastMessage ? getEntrySignature(session.lastMessage) : null,
  };
}

function updateHistoryCursor(
  history: RelayHistoryEntry[],
  previous: HistoryCursor,
): HistoryCursor {
  const lastEntry = history.at(-1);
  if (!lastEntry) {
    return {
      ...previous,
      historySize: 0,
    };
  }

  return {
    historySize: history.length,
    receivedAt: lastEntry.receivedAt,
    signature: getEntrySignature(lastEntry),
  };
}

function getEntriesAfterCursor(
  history: RelayHistoryEntry[],
  cursor: HistoryCursor,
): RelayHistoryEntry[] {
  if (history.length === 0) {
    return [];
  }

  if (cursor.signature) {
    const existingIndex = history.findIndex(
      (entry) => getEntrySignature(entry) === cursor.signature,
    );
    if (existingIndex >= 0) {
      return history.slice(existingIndex + 1);
    }
  }

  if (cursor.receivedAt) {
    const receivedAt = cursor.receivedAt;
    return history.filter((entry) => entry.receivedAt > receivedAt);
  }

  return history.slice(cursor.historySize);
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

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}
