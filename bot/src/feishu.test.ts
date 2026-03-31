import { describe, expect, it, vi } from "vitest";

import { FeishuGateway } from "./feishu.js";

describe("FeishuGateway", () => {
  it("connects via WSClient, dispatches text messages, and sends replies", async () => {
    const startedDispatchers: Array<{ handles: Record<string, Function> }> = [];
    const sentMessages: unknown[] = [];
    const closed = vi.fn();

    class MockEventDispatcher {
      public readonly handles: Record<string, Function> = {};

      register(handles: Record<string, Function>) {
        Object.assign(this.handles, handles);
        return this;
      }
    }

    class MockWSClient {
      async start(params: { eventDispatcher: unknown }) {
        startedDispatchers.push(
          params.eventDispatcher as { handles: Record<string, Function> },
        );
      }

      close() {
        closed();
      }
    }

    class MockClient {
      public readonly im = {
        v1: {
          message: {
            create: vi.fn(async (payload: unknown) => {
              sentMessages.push(payload);
            }),
          },
        },
      };
    }

    const onTextMessage = vi.fn();
    const gateway = new FeishuGateway(
      {
        appId: "app-id",
        appSecret: "app-secret",
      },
      {
        Client: MockClient,
        WSClient: MockWSClient,
        EventDispatcher: MockEventDispatcher,
      },
    );

    await gateway.start({ onTextMessage });

    expect(startedDispatchers).toHaveLength(1);

    await startedDispatchers[0].handles["im.message.receive_v1"]({
      sender: {
        sender_id: {
          user_id: "user-1",
        },
      },
      message: {
        message_id: "message-1",
        chat_id: "chat-1",
        message_type: "text",
        content: JSON.stringify({
          text: "/list",
        }),
      },
    });

    expect(onTextMessage).toHaveBeenCalledWith({
      userId: "user-1",
      chatId: "chat-1",
      messageId: "message-1",
      text: "/list",
    });

    await gateway.sendText("chat-1", "hello from bot");

    expect(sentMessages).toEqual([
      {
        params: {
          receive_id_type: "chat_id",
        },
        data: {
          receive_id: "chat-1",
          msg_type: "text",
          content: JSON.stringify({
            text: "hello from bot",
          }),
        },
      },
    ]);

    gateway.close();
    expect(closed).toHaveBeenCalledTimes(1);
  });

  it("ignores malformed and non-text Feishu events", async () => {
    const dispatchers: Array<{ handles: Record<string, Function> }> = [];

    class MockEventDispatcher {
      public readonly handles: Record<string, Function> = {};

      register(handles: Record<string, Function>) {
        Object.assign(this.handles, handles);
        return this;
      }
    }

    class MockWSClient {
      async start(params: { eventDispatcher: unknown }) {
        dispatchers.push(
          params.eventDispatcher as { handles: Record<string, Function> },
        );
      }

      close() {}
    }

    class MockClient {
      public readonly im = {
        v1: {
          message: {
            create: vi.fn(),
          },
        },
      };
    }

    const onTextMessage = vi.fn();
    const gateway = new FeishuGateway(
      {
        appId: "app-id",
        appSecret: "app-secret",
      },
      {
        Client: MockClient,
        WSClient: MockWSClient,
        EventDispatcher: MockEventDispatcher,
      },
    );

    await gateway.start({ onTextMessage });

    await dispatchers[0].handles["im.message.receive_v1"]({
      sender: {
        sender_id: {
          user_id: "user-1",
        },
      },
      message: {
        message_id: "message-1",
        chat_id: "chat-1",
        message_type: "image",
        content: "{}",
      },
    });

    await dispatchers[0].handles["im.message.receive_v1"]({
      sender: {
        sender_id: {
          user_id: "user-1",
        },
      },
      message: {
        message_id: "message-2",
        chat_id: "chat-1",
        message_type: "text",
        content: "{not json",
      },
    });

    expect(onTextMessage).not.toHaveBeenCalled();
  });
});
