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

export class BotService {
  private readonly attachments = new Map<string, UserAttachment>();

  constructor(
    private readonly relayClient: RelayClientLike,
    private readonly messenger: BotMessenger,
  ) {}

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
    const matchedSession = findSession(sessions, sessionQuery);
    if (!matchedSession) {
      await this.reply(chatId, `Session "${sessionQuery}" not found.`);
      return;
    }

    const previousAttachment = this.attachments.get(userId);
    if (
      previousAttachment &&
      previousAttachment.sessionId !== matchedSession.sessionId
    ) {
      await this.relayClient.detach(previousAttachment.sessionId);
    }

    const attachedSession = await this.relayClient.attach(
      matchedSession.sessionId,
      userId,
    );

    this.attachments.set(userId, {
      sessionId: attachedSession.sessionId,
      sessionName: attachedSession.displayName,
      chatId,
    });

    await this.reply(
      chatId,
      `Attached to ${formatSessionTag(attachedSession.displayName)}.`,
    );
  }

  private async handleDetach(userId: string, chatId: string): Promise<void> {
    const attachment = this.attachments.get(userId);
    if (!attachment) {
      await this.reply(chatId, "You are not attached to a session.");
      return;
    }

    await this.relayClient.detach(attachment.sessionId);
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
      [
        `Session: ${formatSessionTag(session.displayName)}`,
        `State: ${session.state}`,
        `Online: ${session.online ? "yes" : "no"}`,
        `Turns: ${session.turnCount}`,
      ].join("\n"),
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

function findSession(
  sessions: RelaySessionSummary[],
  query: string,
): RelaySessionSummary | undefined {
  const normalizedQuery = query.trim().toLowerCase();
  return sessions.find((session) => {
    return (
      session.sessionId.toLowerCase() === normalizedQuery ||
      session.displayName.toLowerCase() === normalizedQuery
    );
  });
}
