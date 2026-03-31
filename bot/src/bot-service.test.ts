import { describe, expect, it, vi } from "vitest";

import { BotService } from "./bot-service.js";

describe("BotService", () => {
  it("lists available sessions", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
        createSessionDetail({
          sessionId: "session-2",
          displayName: "workspace-b",
          online: false,
        }),
      ]),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/list",
    });

    expect(relay.listSessions).toHaveBeenCalledTimes(1);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-a"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("workspace-b"),
    );
  });

  it("attaches to a session and forwards plain text as a prompt", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
      ]),
      attach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach workspace-a",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "hello\nremote world",
    });

    expect(relay.attach).toHaveBeenCalledWith("session-1", "user-1");
    expect(relay.sendPrompt).toHaveBeenCalledWith(
      "session-1",
      "hello\nremote world",
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      "Attached to [workspace-a].",
    );
  });

  it("uses the current attachment for status and history commands", async () => {
    const relay = createRelayDouble({
      listSessions: vi.fn().mockResolvedValue([
        createSessionDetail({
          sessionId: "session-1",
          displayName: "workspace-a",
        }),
      ]),
      attach: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
      getSession: vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            state: "executing",
            turnCount: 3,
          }),
        ),
      getHistory: vi.fn().mockResolvedValue([
        {
          direction: "out",
          classification: "agentMessage",
          method: "item/agentMessage/delta",
          raw: "assistant output",
          payload: { text: "assistant output" },
          threadId: "thread-1",
          turnId: "turn-1",
          receivedAt: "2026-03-31T00:00:00.000Z",
        },
      ]),
    });
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/attach session-1",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "/status",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-3",
      text: "/history 1",
    });

    expect(relay.getSession).toHaveBeenCalledWith("session-1");
    expect(relay.getHistory).toHaveBeenCalledWith("session-1", 1);
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("State: executing"),
    );
    expect(messenger.sendText).toHaveBeenCalledWith(
      "chat-1",
      expect.stringContaining("[workspace-a]\n```text\n"),
    );
  });

  it("returns user-friendly errors for unknown commands and detached prompts", async () => {
    const relay = createRelayDouble();
    const messenger = createMessengerDouble();
    const service = new BotService(relay, messenger);

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/unknown",
    });

    await service.handleTextMessage({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-2",
      text: "hello",
    });

    expect(relay.sendPrompt).not.toHaveBeenCalled();
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      1,
      "chat-1",
      'Unknown command "/unknown".',
    );
    expect(messenger.sendText).toHaveBeenNthCalledWith(
      2,
      "chat-1",
      "Attach to a session first with /attach <session>.",
    );
  });
});

function createRelayDouble(
  overrides: Partial<{
    listSessions: ReturnType<typeof vi.fn>;
    getSession: ReturnType<typeof vi.fn>;
    getHistory: ReturnType<typeof vi.fn>;
    sendPrompt: ReturnType<typeof vi.fn>;
    sendApproval: ReturnType<typeof vi.fn>;
    interrupt: ReturnType<typeof vi.fn>;
    attach: ReturnType<typeof vi.fn>;
    detach: ReturnType<typeof vi.fn>;
  }> = {},
) {
  return {
    listSessions: overrides.listSessions ?? vi.fn().mockResolvedValue([]),
    getSession:
      overrides.getSession ??
      vi.fn().mockResolvedValue(createSessionDetail({ sessionId: "session-1" })),
    getHistory: overrides.getHistory ?? vi.fn().mockResolvedValue([]),
    sendPrompt: overrides.sendPrompt ?? vi.fn().mockResolvedValue(undefined),
    sendApproval:
      overrides.sendApproval ?? vi.fn().mockResolvedValue(undefined),
    interrupt: overrides.interrupt ?? vi.fn().mockResolvedValue(undefined),
    attach:
      overrides.attach ??
      vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: "user-1",
          }),
        ),
    detach:
      overrides.detach ??
      vi
        .fn()
        .mockResolvedValue(
          createSessionDetail({
            sessionId: "session-1",
            displayName: "workspace-a",
            attachedUser: null,
          }),
        ),
  };
}

function createMessengerDouble() {
  return {
    sendText: vi.fn().mockResolvedValue(undefined),
  };
}

function createSessionDetail(
  overrides: Partial<{
    sessionId: string;
    displayName: string;
    state: "idle" | "executing" | "waitingApproval";
    online: boolean;
    turnCount: number;
    threadId: string | null;
    turnId: string | null;
    attachedUser: string | null;
    metadata: Record<string, unknown>;
    graceExpiresAt: string | null;
    historySize: number;
    lastMessage: unknown;
  }> = {},
) {
  return {
    sessionId: overrides.sessionId ?? "session-1",
    displayName: overrides.displayName ?? "workspace-a",
    state: overrides.state ?? "idle",
    online: overrides.online ?? true,
    turnCount: overrides.turnCount ?? 0,
    threadId: overrides.threadId ?? null,
    turnId: overrides.turnId ?? null,
    attachedUser: overrides.attachedUser ?? null,
    metadata: overrides.metadata ?? {},
    graceExpiresAt: overrides.graceExpiresAt ?? null,
    historySize: overrides.historySize ?? 0,
    lastMessage: overrides.lastMessage ?? null,
  };
}
